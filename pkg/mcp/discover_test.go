package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/calitti/mcp-api-gateway/pkg/storage"
)

func metaParams(version string) json.RawMessage {
	b, _ := json.Marshal(map[string]interface{}{
		"_meta": map[string]interface{}{
			"io.modelcontextprotocol/protocolVersion": version,
		},
	})
	return b
}

func TestNegotiateVersion(t *testing.T) {
	tests := []struct {
		name    string
		params  json.RawMessage
		want    string
		wantErr bool
	}{
		{"absent params defaults to legacy", nil, protocolVersionLegacy, false},
		{"empty meta defaults to legacy", json.RawMessage(`{}`), protocolVersionLegacy, false},
		{"explicit 2026 accepted", metaParams(protocolVersion2026), protocolVersion2026, false},
		{"explicit legacy accepted", metaParams(protocolVersionLegacy), protocolVersionLegacy, false},
		{"non-object params defaults to legacy", json.RawMessage(`"positional"`), protocolVersionLegacy, false},
		{"unsupported version errors", metaParams("2099-01-01"), "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := negotiateVersion(tt.params)
			if tt.wantErr {
				if err == nil {
					t.Fatal("want UnsupportedProtocolVersionError, got nil")
				}
				data, _ := err.Data.(map[string]interface{})
				if data["type"] != "UnsupportedProtocolVersionError" {
					t.Errorf("error data type = %v, want UnsupportedProtocolVersionError", data["type"])
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %+v", err)
			}
			if got != tt.want {
				t.Errorf("version = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHandleRequest_ServerDiscover(t *testing.T) {
	s := &MCPServer{}
	req := &JSONRPCRequest{JSONRPC: "2.0", ID: 1, Method: "server/discover", Params: metaParams(protocolVersion2026)}

	resp := s.handleRequest(context.Background(), "test", "admin", []string{"*"}, req)
	if resp.Error != nil {
		t.Fatalf("server/discover errored: %+v", resp.Error)
	}
	res, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("result not an object: %T", resp.Result)
	}
	if res["resultType"] != "complete" {
		t.Errorf("resultType = %v, want complete", res["resultType"])
	}
	vers, _ := res["supportedVersions"].([]string)
	if len(vers) != 2 || vers[0] != protocolVersion2026 {
		t.Errorf("supportedVersions = %v, want [%s %s]", vers, protocolVersion2026, protocolVersionLegacy)
	}
	meta, _ := res["_meta"].(map[string]interface{})
	if _, ok := meta[metaServerInfo]; !ok {
		t.Errorf("_meta missing %s", metaServerInfo)
	}
	if res["ttlMs"] == nil || res["cacheScope"] != "public" {
		t.Errorf("missing cache hints: ttlMs=%v cacheScope=%v", res["ttlMs"], res["cacheScope"])
	}
}

// tools/list returns the legacy struct shape for a pre-2026 client (no _meta
// version) and the 2026-07-28 map shape (resultType + cache hints) when the
// client negotiates 2026-07-28.
func TestHandleRequest_ToolsListVersionGated(t *testing.T) {
	s, store, _ := newTestServer(t, "master-token-xxxxxxxxxxxxxxxxxxxx")
	conn := store.SeedConnection(&storage.APIConnection{ID: "c1", Name: "w", BaseURL: "https://w.example.com", AuthType: "none", Enabled: true})
	store.SeedEndpoint(&storage.APIEndpoint{ID: "e1", ConnectionID: conn.ID, ToolName: "get_forecast", ToolDescription: "d", Path: "/f", Method: "GET"})

	// Legacy: no _meta version -> ListToolsResponse struct, no cache hints.
	legacy := s.handleRequest(context.Background(), "m", "admin", []string{"*"},
		&JSONRPCRequest{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
	if _, ok := legacy.Result.(ListToolsResponse); !ok {
		t.Fatalf("legacy tools/list result = %T, want ListToolsResponse", legacy.Result)
	}

	// 2026: negotiated version -> map with resultType + cache hints.
	got := s.handleRequest(context.Background(), "m", "admin", []string{"*"},
		&JSONRPCRequest{JSONRPC: "2.0", ID: 2, Method: "tools/list", Params: metaParams(protocolVersion2026)})
	res, ok := got.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("2026 tools/list result = %T, want map", got.Result)
	}
	if res["resultType"] != "complete" || res["cacheScope"] != "public" || res["ttlMs"] != toolsListTTLMs {
		t.Errorf("2026 shape missing fields: %+v", res)
	}
	if _, ok := res["tools"].([]Tool); !ok {
		t.Errorf("2026 tools field = %T, want []Tool", res["tools"])
	}
}

// callResultTTLMs advertises a cache hint only when the tool is a GET AND the
// gateway response cache is enabled — never otherwise (honest hints).
func TestCallResultTTLMs(t *testing.T) {
	s, store, _ := newTestServer(t, "master-token-xxxxxxxxxxxxxxxxxxxx")
	conn := store.SeedConnection(&storage.APIConnection{ID: "c1", Name: "w", BaseURL: "https://w.example.com", AuthType: "none", Enabled: true})
	store.SeedEndpoint(&storage.APIEndpoint{ID: "g1", ConnectionID: conn.ID, ToolName: "get_thing", ToolDescription: "d", Path: "/g", Method: "GET"})
	store.SeedEndpoint(&storage.APIEndpoint{ID: "p1", ConnectionID: conn.ID, ToolName: "post_thing", ToolDescription: "d", Path: "/p", Method: "POST"})

	// Cache disabled: no hint for any method.
	if ms := s.callResultTTLMs(context.Background(), "get_thing"); ms != 0 {
		t.Errorf("cache disabled: ttlMs = %d, want 0", ms)
	}

	// Cache enabled: GET gets the hint, POST does not, unknown/admin does not.
	s.client.EnableResponseCache(30 * time.Second)
	if ms := s.callResultTTLMs(context.Background(), "get_thing"); ms != 30000 {
		t.Errorf("GET with cache: ttlMs = %d, want 30000", ms)
	}
	if ms := s.callResultTTLMs(context.Background(), "post_thing"); ms != 0 {
		t.Errorf("POST: ttlMs = %d, want 0", ms)
	}
	if ms := s.callResultTTLMs(context.Background(), "admin_add_endpoint"); ms != 0 {
		t.Errorf("admin tool: ttlMs = %d, want 0", ms)
	}
}

func TestExtractClientInfo(t *testing.T) {
	mk := func(name, ver string) json.RawMessage {
		b, _ := json.Marshal(map[string]interface{}{
			"_meta": map[string]interface{}{
				"io.modelcontextprotocol/clientInfo": map[string]interface{}{"name": name, "version": ver},
			},
		})
		return b
	}
	tests := []struct {
		name   string
		params json.RawMessage
		want   string
	}{
		{"name and version", mk("claude-code", "2.0"), "claude-code/2.0"},
		{"name only", mk("antigravity", ""), "antigravity"},
		{"no name", mk("", "9.9"), ""},
		{"absent params", nil, ""},
		{"no meta", json.RawMessage(`{}`), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractClientInfo(tt.params); got != tt.want {
				t.Errorf("extractClientInfo() = %q, want %q", got, tt.want)
			}
		})
	}
}

// An explicitly unsupported version is rejected before method dispatch, on any
// method — here tools/call, which would otherwise try to execute.
func TestHandleRequest_UnsupportedVersionRejectedBeforeDispatch(t *testing.T) {
	s := &MCPServer{}
	req := &JSONRPCRequest{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: metaParams("2099-01-01")}

	resp := s.handleRequest(context.Background(), "test", "user", []string{"*"}, req)
	if resp.Error == nil {
		t.Fatal("want error for unsupported version, got nil")
	}
	data, _ := resp.Error.Data.(map[string]interface{})
	if data["type"] != "UnsupportedProtocolVersionError" {
		t.Errorf("error type = %v, want UnsupportedProtocolVersionError", data["type"])
	}
}
