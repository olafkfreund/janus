// Package toolintegrity provides content-hash pinning of MCP tool
// definitions so a client (or the gateway itself) can detect when a tool's
// behaviour-relevant fields have silently changed after the client first
// saw it — the "rug-pull" defense described in issue #10.
//
// A tool's hash is computed over its name, description, HTTP method, path
// template, and JSON-Schema parameters. The hash is stable regardless of
// key ordering inside the JSON-Schema string and independent of
// surrounding whitespace, so cosmetic re-serialization of a schema does
// not trigger a false positive.
package toolintegrity

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// ToolDef is the canonical, hashable view of a tool definition.
type ToolDef struct {
	Name             string
	Description      string
	Method           string
	Path             string // path template with {param} placeholders
	ParametersSchema string // JSON-Schema string
}

// canonicalForm is the fixed-shape struct fed into the hash. Using named
// JSON fields (rather than concatenating raw strings) rules out any
// ambiguity between where one field ends and the next begins.
type canonicalForm struct {
	Name             string `json:"name"`
	Description      string `json:"description"`
	Method           string `json:"method"`
	Path             string `json:"path"`
	ParametersSchema string `json:"parameters_schema"`
}

// Hash returns a deterministic hex SHA-256 digest over the canonicalized
// definition. ParametersSchema is canonicalized independently: if it
// parses as JSON, it is re-encoded with object keys sorted recursively and
// no incidental whitespace, so differently-ordered-but-equal schemas hash
// identically; if it is not valid JSON, the raw string is hashed verbatim.
func Hash(d ToolDef) string {
	cf := canonicalForm{
		Name:             d.Name,
		Description:      d.Description,
		Method:           d.Method,
		Path:             d.Path,
		ParametersSchema: canonicalizeSchema(d.ParametersSchema),
	}

	// cf is a plain struct of strings, so json.Marshal cannot fail here.
	b, _ := json.Marshal(cf)

	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Changed reports whether d's current hash differs from a previously
// stored hash. An empty priorHash means "no baseline" and returns false —
// there is nothing to compare against yet.
func Changed(priorHash string, d ToolDef) bool {
	if priorHash == "" {
		return false
	}
	return Hash(d) != priorHash
}

// canonicalizeSchema returns a canonical JSON encoding of schema — object
// keys sorted recursively, arrays left in their original order, and no
// extraneous whitespace — when schema is valid JSON. If schema fails to
// parse, it is returned unchanged so callers still get a deterministic
// (if less forgiving) hash over the raw text.
func canonicalizeSchema(schema string) string {
	var v interface{}
	if err := json.Unmarshal([]byte(schema), &v); err != nil {
		return schema
	}

	var buf bytes.Buffer
	writeCanonical(&buf, v)
	return buf.String()
}

// writeCanonical recursively writes v to buf as canonical JSON: object
// keys are sorted lexicographically, array element order is preserved,
// and scalars are encoded via encoding/json.
func writeCanonical(buf *bytes.Buffer, v interface{}) {
	switch val := v.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeJSONScalar(buf, k)
			buf.WriteByte(':')
			writeCanonical(buf, val[k])
		}
		buf.WriteByte('}')

	case []interface{}:
		buf.WriteByte('[')
		for i, elem := range val {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeCanonical(buf, elem)
		}
		buf.WriteByte(']')

	default:
		// Strings, numbers (float64), bools, and nil.
		writeJSONScalar(buf, val)
	}
}

// writeJSONScalar writes v's standard JSON encoding to buf.
func writeJSONScalar(buf *bytes.Buffer, v interface{}) {
	// encoding/json cannot fail on the scalar types produced by
	// json.Unmarshal into interface{} (string, float64, bool, nil).
	b, _ := json.Marshal(v)
	buf.Write(b)
}
