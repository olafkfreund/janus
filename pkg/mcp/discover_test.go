package mcp

import (
	"context"
	"encoding/json"
	"testing"
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
