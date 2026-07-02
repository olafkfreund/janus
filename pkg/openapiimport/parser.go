package openapiimport

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	yaml "go.yaml.in/yaml/v2"
)

// httpMethods lists the OpenAPI path-item keys that denote operations, in a
// fixed, deterministic processing order. This order (rather than map
// iteration order) is what makes generated tool names reproducible across
// runs.
var httpMethods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}

var nonAlnumRE = regexp.MustCompile(`[^a-z0-9]+`)

// openAPIRoot is the subset of an OpenAPI 3.x document this package cares
// about.
type openAPIRoot struct {
	OpenAPI string `json:"openapi"`
	Info    struct {
		Title string `json:"title"`
	} `json:"info"`
	Servers []struct {
		URL string `json:"url"`
	} `json:"servers"`
	Paths      map[string]json.RawMessage `json:"paths"`
	Components struct {
		SecuritySchemes map[string]securityScheme `json:"securitySchemes"`
	} `json:"components"`
}

// pathItemMeta captures the parameters shared by every operation under a
// path (OpenAPI's path-item-level "parameters").
type pathItemMeta struct {
	Parameters []parameter `json:"parameters"`
}

type operation struct {
	OperationID string       `json:"operationId"`
	Summary     string       `json:"summary"`
	Description string       `json:"description"`
	Parameters  []parameter  `json:"parameters"`
	RequestBody *requestBody `json:"requestBody"`
}

type parameter struct {
	Name        string                 `json:"name"`
	In          string                 `json:"in"`
	Description string                 `json:"description"`
	Required    bool                   `json:"required"`
	Schema      map[string]interface{} `json:"schema"`
}

type requestBody struct {
	Required bool                 `json:"required"`
	Content  map[string]mediaType `json:"content"`
}

type mediaType struct {
	Schema map[string]interface{} `json:"schema"`
}

type securityScheme struct {
	Type   string `json:"type"`
	Scheme string `json:"scheme"`
	In     string `json:"in"`
}

// Parse decodes an OpenAPI 3.x document (JSON, or YAML via the yaml decoder
// already vendored by this module) into a ParsedConnection and its tools.
func Parse(spec []byte, opts Options) (*ParsedConnection, error) {
	jsonBytes, err := normalizeToJSON(spec)
	if err != nil {
		return nil, err
	}

	var root openAPIRoot
	if err := json.Unmarshal(jsonBytes, &root); err != nil {
		return nil, fmt.Errorf("openapiimport: decoding OpenAPI document: %w", err)
	}
	if !strings.HasPrefix(root.OpenAPI, "3.") {
		return nil, fmt.Errorf("openapiimport: unsupported OpenAPI version %q: only 3.x specs are supported", root.OpenAPI)
	}

	conn := &ParsedConnection{
		Name:     sanitizeConnectionName(root.Info.Title),
		AuthType: inferAuthType(root.Components.SecuritySchemes),
	}
	if len(root.Servers) > 0 {
		conn.BaseURL = root.Servers[0].URL
	}

	paths := make([]string, 0, len(root.Paths))
	for p := range root.Paths {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	nameCounts := map[string]int{}
	for _, p := range paths {
		raw := root.Paths[p]

		var meta pathItemMeta
		if err := json.Unmarshal(raw, &meta); err != nil {
			return nil, fmt.Errorf("openapiimport: decoding path item %q: %w", p, err)
		}
		var methodOps map[string]json.RawMessage
		if err := json.Unmarshal(raw, &methodOps); err != nil {
			return nil, fmt.Errorf("openapiimport: decoding path item %q: %w", p, err)
		}

		for _, method := range httpMethods {
			opRaw, ok := methodOps[method]
			if !ok {
				continue
			}
			var op operation
			if err := json.Unmarshal(opRaw, &op); err != nil {
				return nil, fmt.Errorf("openapiimport: decoding operation %s %s: %w", strings.ToUpper(method), p, err)
			}

			schema, err := buildParametersSchema(mergeParameters(meta.Parameters, op.Parameters), op.RequestBody)
			if err != nil {
				return nil, fmt.Errorf("openapiimport: building parameters schema for %s %s: %w", strings.ToUpper(method), p, err)
			}

			base := opts.ToolNamePrefix + toolNameBase(op.OperationID, method, p)
			name := uniqueName(base, nameCounts)

			conn.Endpoints = append(conn.Endpoints, ParsedEndpoint{
				ToolName:         name,
				ToolDescription:  firstNonEmpty(op.Summary, op.Description),
				Path:             p,
				Method:           strings.ToUpper(method),
				ParametersSchema: schema,
			})
		}
	}

	return conn, nil
}

// normalizeToJSON returns spec as JSON bytes. JSON input passes through
// unchanged; YAML input is decoded and re-encoded as JSON using the yaml
// decoder already vendored by this module (go.yaml.in/yaml/v2).
func normalizeToJSON(spec []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(spec)
	if len(trimmed) == 0 {
		return nil, errors.New("openapiimport: empty spec")
	}
	if json.Valid(trimmed) {
		return trimmed, nil
	}

	var raw interface{}
	if err := yaml.Unmarshal(trimmed, &raw); err != nil {
		return nil, fmt.Errorf("openapiimport: spec is neither valid JSON nor valid YAML: %w", err)
	}
	out, err := json.Marshal(convertYAMLValue(raw))
	if err != nil {
		return nil, fmt.Errorf("openapiimport: converting YAML spec to JSON: %w", err)
	}
	return out, nil
}

// convertYAMLValue recursively converts the map[interface{}]interface{}
// values produced by yaml.v2 into map[string]interface{}, which is the only
// map shape encoding/json can marshal.
func convertYAMLValue(v interface{}) interface{} {
	switch t := v.(type) {
	case map[interface{}]interface{}:
		m := make(map[string]interface{}, len(t))
		for k, val := range t {
			m[fmt.Sprint(k)] = convertYAMLValue(val)
		}
		return m
	case map[string]interface{}:
		for k, val := range t {
			t[k] = convertYAMLValue(val)
		}
		return t
	case []interface{}:
		for i, item := range t {
			t[i] = convertYAMLValue(item)
		}
		return t
	default:
		return v
	}
}

// mergeParameters combines path-item-level parameters with operation-level
// parameters, keyed by (in, name) as required by the OpenAPI spec.
// Operation-level entries override path-level entries with the same key.
func mergeParameters(base, override []parameter) []parameter {
	key := func(p parameter) string { return p.In + "|" + p.Name }

	merged := make([]parameter, 0, len(base)+len(override))
	index := make(map[string]int, len(base)+len(override))
	for _, p := range base {
		index[key(p)] = len(merged)
		merged = append(merged, p)
	}
	for _, p := range override {
		k := key(p)
		if i, ok := index[k]; ok {
			merged[i] = p
			continue
		}
		index[k] = len(merged)
		merged = append(merged, p)
	}
	return merged
}

// buildParametersSchema builds a JSON Schema object document (encoded as a
// JSON string) describing params plus, if present, an inlined
// application/json request body schema.
func buildParametersSchema(params []parameter, rb *requestBody) (string, error) {
	properties := map[string]interface{}{}
	var required []string

	for _, p := range params {
		if p.Name == "" {
			continue
		}
		properties[p.Name] = parameterToJSONSchema(p)

		req := p.Required
		if p.In == "path" {
			req = true // path parameters are always required per OpenAPI 3.x
		}
		if req {
			required = append(required, p.Name)
		}
	}

	if rb != nil {
		if mt, ok := rb.Content["application/json"]; ok && mt.Schema != nil {
			bodyType, _ := mt.Schema["type"].(string)
			if bodyType == "" || bodyType == "object" {
				if bodyProps, ok := mt.Schema["properties"].(map[string]interface{}); ok {
					for name, propSchema := range bodyProps {
						properties[name] = propSchema
					}
				}
				if reqList, ok := mt.Schema["required"].([]interface{}); ok {
					for _, r := range reqList {
						if s, ok := r.(string); ok {
							required = append(required, s)
						}
					}
				}
			} else {
				// Non-object bodies (arrays, scalars) can't be flattened
				// into top-level properties, so inline them under "body".
				properties["body"] = mt.Schema
				if rb.Required {
					required = append(required, "body")
				}
			}
		}
	}

	schema := map[string]interface{}{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = dedupeSorted(required)
	}

	b, err := json.Marshal(schema)
	if err != nil {
		return "", fmt.Errorf("marshaling schema: %w", err)
	}
	return string(b), nil
}

// parameterToJSONSchema converts a single OpenAPI parameter into a JSON
// Schema property definition.
func parameterToJSONSchema(p parameter) map[string]interface{} {
	propType := "string"
	if p.Schema != nil {
		if t, ok := p.Schema["type"].(string); ok && t != "" {
			propType = t
		}
	}
	prop := map[string]interface{}{"type": propType}
	if p.Description != "" {
		prop["description"] = p.Description
	}
	if p.Schema != nil {
		if f, ok := p.Schema["format"].(string); ok && f != "" {
			prop["format"] = f
		}
		if enum, ok := p.Schema["enum"]; ok {
			prop["enum"] = enum
		}
	}
	return prop
}

// dedupeSorted removes duplicate strings and returns the result sorted, so
// that the generated JSON Schema is byte-for-byte reproducible.
func dedupeSorted(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

// inferAuthType makes a best-effort guess at the connection's auth
// mechanism from its OpenAPI security schemes. When multiple schemes are
// present, the most specific/common one wins: bearer > basic >
// apiKey/oauth2/openIdConnect (all mapped to "custom_headers").
func inferAuthType(schemes map[string]securityScheme) string {
	if len(schemes) == 0 {
		return "none"
	}

	names := make([]string, 0, len(schemes))
	for name := range schemes {
		names = append(names, name)
	}
	sort.Strings(names)

	authType := "none"
	bestRank := 99
	for _, name := range names {
		s := schemes[name]
		var t string
		var rank int
		switch {
		case strings.EqualFold(s.Type, "http") && strings.EqualFold(s.Scheme, "bearer"):
			t, rank = "bearer", 0
		case strings.EqualFold(s.Type, "http") && strings.EqualFold(s.Scheme, "basic"):
			t, rank = "basic", 1
		case strings.EqualFold(s.Type, "apiKey"):
			t, rank = "custom_headers", 2
		case strings.EqualFold(s.Type, "oauth2"), strings.EqualFold(s.Type, "openIdConnect"):
			t, rank = "custom_headers", 3
		default:
			continue
		}
		if rank < bestRank {
			bestRank = rank
			authType = t
		}
	}
	return authType
}

// sanitizeConnectionName trims and collapses whitespace in an OpenAPI
// info.title so it's safe to use as a display name.
func sanitizeConnectionName(title string) string {
	fields := strings.Fields(title)
	if len(fields) == 0 {
		return "Untitled API"
	}
	return strings.Join(fields, " ")
}

// sanitizeName lowercases s and collapses every run of characters outside
// [a-z0-9] into a single underscore, trimming leading/trailing underscores.
func sanitizeName(s string) string {
	lower := strings.ToLower(s)
	sanitized := nonAlnumRE.ReplaceAllString(lower, "_")
	return strings.Trim(sanitized, "_")
}

// toolNameBase derives the un-prefixed, un-deduplicated tool name for an
// operation: its sanitized operationId, or "<method>_<sanitized_path>" when
// operationId is absent.
func toolNameBase(operationID, method, path string) string {
	if s := sanitizeName(operationID); s != "" {
		return s
	}
	return sanitizeName(method + "_" + path)
}

// uniqueName returns base the first time it's seen, and base with a "_2",
// "_3", ... suffix on each subsequent collision, tracked via counts.
func uniqueName(base string, counts map[string]int) string {
	counts[base]++
	n := counts[base]
	if n == 1 {
		return base
	}
	return fmt.Sprintf("%s_%d", base, n)
}

// firstNonEmpty returns the first non-empty string among a and b.
func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
