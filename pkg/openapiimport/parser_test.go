package openapiimport

import (
	"encoding/json"
	"strings"
	"testing"
)

// petstoreSpec is a small OpenAPI 3.x document covering: a query parameter,
// a required path parameter, a JSON request body, a summary/description
// fallback, a bearer security scheme, and a deliberate operationId
// collision (used to exercise the "_2" suffixing rule).
const petstoreSpec = `{
  "openapi": "3.0.3",
  "info": { "title": "  Pet Store   API  " },
  "servers": [ { "url": "https://petstore.example.com/v1" } ],
  "components": {
    "securitySchemes": {
      "bearerAuth": { "type": "http", "scheme": "bearer" }
    }
  },
  "paths": {
    "/pets": {
      "get": {
        "operationId": "listPets",
        "summary": "List all pets",
        "parameters": [
          {
            "name": "limit",
            "in": "query",
            "description": "How many items to return",
            "required": false,
            "schema": { "type": "integer" }
          }
        ]
      },
      "post": {
        "operationId": "createPet",
        "summary": "Create a pet",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "name": { "type": "string" },
                  "tag": { "type": "string" }
                },
                "required": ["name"]
              }
            }
          }
        }
      }
    },
    "/pets/{petId}": {
      "get": {
        "operationId": "showPetById",
        "summary": "Info for a specific pet",
        "parameters": [
          {
            "name": "petId",
            "in": "path",
            "required": true,
            "schema": { "type": "string" }
          }
        ]
      },
      "tags": ["ignored path-level field"]
    },
    "/pets/{petId}/tags": {
      "get": {
        "operationId": "listPets",
        "description": "Fallback to description when summary is absent"
      }
    },
    "/pets/summary": {
      "get": {
        "description": "Pet summary stats"
      }
    }
  }
}`

func TestParse_Petstore(t *testing.T) {
	conn, err := Parse([]byte(petstoreSpec), Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if got, want := conn.Name, "Pet Store API"; got != want {
		t.Errorf("Name = %q, want %q", got, want)
	}
	if got, want := conn.BaseURL, "https://petstore.example.com/v1"; got != want {
		t.Errorf("BaseURL = %q, want %q", got, want)
	}
	if got, want := conn.AuthType, "bearer"; got != want {
		t.Errorf("AuthType = %q, want %q", got, want)
	}
	if got, want := len(conn.Endpoints), 5; got != want {
		t.Fatalf("len(Endpoints) = %d, want %d", got, want)
	}

	byName := make(map[string]ParsedEndpoint, len(conn.Endpoints))
	for _, ep := range conn.Endpoints {
		if _, dup := byName[ep.ToolName]; dup {
			t.Errorf("duplicate ToolName %q in output", ep.ToolName)
		}
		byName[ep.ToolName] = ep
	}

	wantNames := []string{"listpets", "createpet", "showpetbyid", "listpets_2", "get_pets_summary"}
	for _, name := range wantNames {
		if _, ok := byName[name]; !ok {
			t.Errorf("missing expected ToolName %q; got names %v", name, keysOf(byName))
		}
	}

	t.Run("listPets query parameter", func(t *testing.T) {
		ep := byName["listpets"]
		if ep.Method != "GET" {
			t.Errorf("Method = %q, want GET", ep.Method)
		}
		if ep.Path != "/pets" {
			t.Errorf("Path = %q, want /pets", ep.Path)
		}
		if ep.ToolDescription != "List all pets" {
			t.Errorf("ToolDescription = %q, want %q", ep.ToolDescription, "List all pets")
		}
		schema := decodeSchema(t, ep.ParametersSchema)
		props := schema["properties"].(map[string]interface{})
		limit, ok := props["limit"].(map[string]interface{})
		if !ok {
			t.Fatalf("properties.limit missing, got %v", props)
		}
		if limit["type"] != "integer" {
			t.Errorf("properties.limit.type = %v, want integer", limit["type"])
		}
		if limit["description"] != "How many items to return" {
			t.Errorf("properties.limit.description = %v", limit["description"])
		}
		if _, hasRequired := schema["required"]; hasRequired {
			t.Errorf("required = %v, want absent (limit is optional)", schema["required"])
		}
	})

	t.Run("createPet request body inlined and required", func(t *testing.T) {
		ep := byName["createpet"]
		if ep.Method != "POST" {
			t.Errorf("Method = %q, want POST", ep.Method)
		}
		schema := decodeSchema(t, ep.ParametersSchema)
		props := schema["properties"].(map[string]interface{})
		if _, ok := props["name"]; !ok {
			t.Errorf("properties.name missing, got %v", props)
		}
		if _, ok := props["tag"]; !ok {
			t.Errorf("properties.tag missing, got %v", props)
		}
		required := toStringSlice(t, schema["required"])
		if !contains(required, "name") {
			t.Errorf("required = %v, want to contain %q", required, "name")
		}
	})

	t.Run("showPetById path parameter forced required", func(t *testing.T) {
		ep := byName["showpetbyid"]
		if ep.Path != "/pets/{petId}" {
			t.Errorf("Path = %q, want /pets/{petId}", ep.Path)
		}
		schema := decodeSchema(t, ep.ParametersSchema)
		props := schema["properties"].(map[string]interface{})
		petID, ok := props["petId"].(map[string]interface{})
		if !ok {
			t.Fatalf("properties.petId missing, got %v", props)
		}
		if petID["type"] != "string" {
			t.Errorf("properties.petId.type = %v, want string", petID["type"])
		}
		required := toStringSlice(t, schema["required"])
		if !contains(required, "petId") {
			t.Errorf("required = %v, want to contain petId (path params are always required)", required)
		}
	})

	t.Run("operationId collision suffixed", func(t *testing.T) {
		ep := byName["listpets_2"]
		if ep.Path != "/pets/{petId}/tags" {
			t.Errorf("Path = %q, want /pets/{petId}/tags", ep.Path)
		}
		if ep.ToolDescription != "Fallback to description when summary is absent" {
			t.Errorf("ToolDescription = %q", ep.ToolDescription)
		}
	})

	t.Run("fallback tool name from method+path", func(t *testing.T) {
		ep := byName["get_pets_summary"]
		if ep.Path != "/pets/summary" {
			t.Errorf("Path = %q, want /pets/summary", ep.Path)
		}
		if ep.ToolDescription != "Pet summary stats" {
			t.Errorf("ToolDescription = %q, want description fallback", ep.ToolDescription)
		}
	})
}

func TestParse_ToolNamePrefix(t *testing.T) {
	conn, err := Parse([]byte(petstoreSpec), Options{ToolNamePrefix: "gh_"})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	for _, ep := range conn.Endpoints {
		if !strings.HasPrefix(ep.ToolName, "gh_") {
			t.Errorf("ToolName %q missing prefix %q", ep.ToolName, "gh_")
		}
	}
}

func TestParse_YAML(t *testing.T) {
	const yamlSpec = `
openapi: "3.0.0"
info:
  title: YAML Pets
servers:
  - url: https://yaml.example.com
paths:
  /pets:
    get:
      operationId: listPets
      summary: List pets
`
	conn, err := Parse([]byte(yamlSpec), Options{})
	if err != nil {
		t.Fatalf("Parse() YAML error = %v", err)
	}
	if conn.Name != "YAML Pets" {
		t.Errorf("Name = %q, want %q", conn.Name, "YAML Pets")
	}
	if conn.BaseURL != "https://yaml.example.com" {
		t.Errorf("BaseURL = %q", conn.BaseURL)
	}
	if len(conn.Endpoints) != 1 || conn.Endpoints[0].ToolName != "listpets" {
		t.Errorf("Endpoints = %+v", conn.Endpoints)
	}
}

func TestParse_Errors(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		wantErr string
	}{
		{
			name:    "empty spec",
			spec:    "",
			wantErr: "empty spec",
		},
		{
			name:    "garbage input",
			spec:    "{not json or yaml: [",
			wantErr: "neither valid JSON nor valid YAML",
		},
		{
			name:    "unsupported swagger 2.0",
			spec:    `{"swagger":"2.0","info":{"title":"Old API"},"paths":{}}`,
			wantErr: "unsupported OpenAPI version",
		},
		{
			name:    "openapi 2.x explicit",
			spec:    `{"openapi":"2.0","info":{"title":"Old API"},"paths":{}}`,
			wantErr: "unsupported OpenAPI version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.spec), Options{})
			if err == nil {
				t.Fatalf("Parse() error = nil, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Parse() error = %q, want to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestParse_NoSecuritySchemes(t *testing.T) {
	const spec = `{
		"openapi": "3.1.0",
		"info": { "title": "Open API" },
		"paths": {}
	}`
	conn, err := Parse([]byte(spec), Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if conn.AuthType != "none" {
		t.Errorf("AuthType = %q, want none", conn.AuthType)
	}
	if conn.BaseURL != "" {
		t.Errorf("BaseURL = %q, want empty (no servers)", conn.BaseURL)
	}
	if len(conn.Endpoints) != 0 {
		t.Errorf("Endpoints = %v, want empty", conn.Endpoints)
	}
}

func TestInferAuthType(t *testing.T) {
	tests := []struct {
		name    string
		schemes map[string]securityScheme
		want    string
	}{
		{"none", nil, "none"},
		{"bearer", map[string]securityScheme{"a": {Type: "http", Scheme: "bearer"}}, "bearer"},
		{"basic", map[string]securityScheme{"a": {Type: "http", Scheme: "basic"}}, "basic"},
		{"apiKey", map[string]securityScheme{"a": {Type: "apiKey", In: "header", Scheme: ""}}, "custom_headers"},
		{"oauth2", map[string]securityScheme{"a": {Type: "oauth2"}}, "custom_headers"},
		{
			"bearer wins over apiKey",
			map[string]securityScheme{
				"apiKeyAuth": {Type: "apiKey"},
				"bearerAuth": {Type: "http", Scheme: "bearer"},
			},
			"bearer",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := inferAuthType(tt.schemes); got != tt.want {
				t.Errorf("inferAuthType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"listPets", "listpets"},
		{"List Pets!!", "list_pets"},
		{"  --Trim--  ", "trim"},
		{"", ""},
		{"a__b", "a_b"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := sanitizeName(tt.in); got != tt.want {
				t.Errorf("sanitizeName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestToolNameBase(t *testing.T) {
	tests := []struct {
		name        string
		operationID string
		method      string
		path        string
		want        string
	}{
		{"uses operationId", "listPets", "get", "/pets", "listpets"},
		{"falls back to method+path", "", "get", "/pets/{petId}", "get_pets_petid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toolNameBase(tt.operationID, tt.method, tt.path); got != tt.want {
				t.Errorf("toolNameBase() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUniqueName(t *testing.T) {
	counts := map[string]int{}
	got := []string{
		uniqueName("pets", counts),
		uniqueName("pets", counts),
		uniqueName("pets", counts),
		uniqueName("other", counts),
	}
	want := []string{"pets", "pets_2", "pets_3", "other"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("uniqueName() call %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// --- test helpers ---

func decodeSchema(t *testing.T, s string) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("ParametersSchema is not valid JSON: %v\nschema: %s", err, s)
	}
	if m["type"] != "object" {
		t.Errorf("schema.type = %v, want object", m["type"])
	}
	if _, ok := m["properties"]; !ok {
		t.Errorf("schema missing properties: %s", s)
	}
	return m
}

func toStringSlice(t *testing.T, v interface{}) []string {
	t.Helper()
	raw, ok := v.([]interface{})
	if !ok {
		t.Fatalf("expected []interface{}, got %T (%v)", v, v)
	}
	out := make([]string, len(raw))
	for i, item := range raw {
		s, ok := item.(string)
		if !ok {
			t.Fatalf("expected string element, got %T (%v)", item, item)
		}
		out[i] = s
	}
	return out
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func keysOf(m map[string]ParsedEndpoint) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
