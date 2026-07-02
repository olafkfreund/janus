// Package openapiimport parses OpenAPI 3.x specifications (JSON, or YAML when
// a YAML decoder is already vendored) into gateway-agnostic connection/tool
// DTOs. It is intentionally self-contained: it does not depend on
// pkg/storage or any CGO-linked package, so it can be used from tooling that
// must remain pure Go.
package openapiimport

// ParsedEndpoint is one OpenAPI operation reshaped into a gateway tool
// definition.
type ParsedEndpoint struct {
	// ToolName is derived from the operation's operationId (sanitized to
	// [a-z0-9_]) or, if absent, from "<method>_<sanitized_path>". Collisions
	// within a single spec are disambiguated with a "_2", "_3", ... suffix.
	ToolName string
	// ToolDescription is the operation's summary, falling back to its
	// description, if present.
	ToolDescription string
	// Path is the OpenAPI path template, e.g. "/pets/{petId}". Path
	// parameter placeholders are preserved verbatim.
	Path string
	// Method is the upper-cased HTTP method, e.g. "GET".
	Method string
	// ParametersSchema is a JSON Schema (object) document, encoded as a
	// JSON string, describing the operation's path/query/header parameters
	// and its JSON request body (if any), inlined into a single set of
	// properties.
	ParametersSchema string
}

// ParsedConnection is an OpenAPI document reshaped into a gateway
// connection: a base URL, a best-effort auth type, and its tools.
type ParsedConnection struct {
	// Name is derived from info.title, sanitized for use as a connection
	// name.
	Name string
	// BaseURL is taken from the first entry in servers, if any.
	BaseURL string
	// AuthType is a best-effort classification of the spec's security
	// schemes: "bearer", "basic", "custom_headers", or "none".
	AuthType string
	// Endpoints holds one ParsedEndpoint per OpenAPI operation, in
	// deterministic (path, then method) order.
	Endpoints []ParsedEndpoint
}

// Options controls Parse's behaviour.
type Options struct {
	// ToolNamePrefix, if set, is prepended to every derived ToolName before
	// collision resolution.
	ToolNamePrefix string
}
