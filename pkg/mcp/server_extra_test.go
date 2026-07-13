package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/calitti/mcp-api-gateway/pkg/storage"
)

// This file extends server_test.go's coverage of the security-critical
// paths in server.go: extractToken's two auth surfaces, listTools' scope
// and admin-role filtering, and ServeMessages' session re-auth (exercised
// as a real HTTP handler rather than just the underlying hash compare).

// --- extractToken -----------------------------------------------------------

func TestExtractToken(t *testing.T) {
	tests := []struct {
		name   string
		header string
		query  string
		want   string
	}{
		{"bearer header", "Bearer abc123", "", "abc123"},
		{"bearer header lowercase scheme", "bearer abc123", "", "abc123"},
		{"query param fallback when no header", "", "xyz789", "xyz789"},
		{"header takes precedence over query", "Bearer fromheader", "fromquery", "fromheader"},
		{"neither present yields empty", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/sse"
			if tt.query != "" {
				url += "?token=" + tt.query
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			if got := extractToken(req); got != tt.want {
				t.Errorf("extractToken() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- listTools: scope filtering + admin visibility --------------------------

func TestListTools_ScopeFiltersEndpoints(t *testing.T) {
	s, store, _ := newTestServer(t, "master-token-xxxxxxxxxxxxxxxxxxxx")
	conn := store.SeedConnection(&storage.APIConnection{ID: "conn-1", Name: "weather", BaseURL: "https://weather.example.com", AuthType: "none", Enabled: true})
	store.SeedEndpoint(&storage.APIEndpoint{ID: "ep-1", ConnectionID: conn.ID, ToolName: "get_forecast", ToolDescription: "d", Path: "/f", Method: "GET"})
	store.SeedEndpoint(&storage.APIEndpoint{ID: "ep-2", ConnectionID: conn.ID, ToolName: "get_billing", ToolDescription: "d", Path: "/b", Method: "GET"})

	tools, err := s.listTools(context.Background(), "user", []string{"get_forecast"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "get_forecast" {
		t.Fatalf("expected only get_forecast to be visible, got %+v", tools)
	}
}

func TestListTools_DisabledConnectionExcluded(t *testing.T) {
	s, store, _ := newTestServer(t, "master-token-xxxxxxxxxxxxxxxxxxxx")
	conn := store.SeedConnection(&storage.APIConnection{ID: "conn-1", Name: "weather", BaseURL: "https://weather.example.com", AuthType: "none", Enabled: false})
	store.SeedEndpoint(&storage.APIEndpoint{ID: "ep-1", ConnectionID: conn.ID, ToolName: "get_forecast", ToolDescription: "d", Path: "/f", Method: "GET"})

	tools, err := s.listTools(context.Background(), "admin", []string{"*"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, tool := range tools {
		if tool.Name == "get_forecast" {
			t.Fatalf("expected endpoint on a disabled connection to be excluded, got %+v", tools)
		}
	}
}

func TestListTools_AdminToolsHiddenForNonAdmin(t *testing.T) {
	s, _, _ := newTestServer(t, "master-token-xxxxxxxxxxxxxxxxxxxx")

	tools, err := s.listTools(context.Background(), "user", []string{"*"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, tool := range tools {
		if strings.HasPrefix(tool.Name, "admin_") {
			t.Errorf("expected no admin_ tools visible for non-admin role, got %q", tool.Name)
		}
	}
}

func TestListTools_AdminToolsVisibleForAdmin(t *testing.T) {
	s, _, _ := newTestServer(t, "master-token-xxxxxxxxxxxxxxxxxxxx")

	tools, err := s.listTools(context.Background(), "admin", []string{"*"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]bool{"admin_add_connection": false, "admin_add_endpoint": false, "admin_register_vault_secret": false}
	for _, tool := range tools {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("expected admin tool %q to be visible for admin role", name)
		}
	}
}

func TestListTools_AdminToolsRespectScopeEvenForAdminRole(t *testing.T) {
	// Admin role alone isn't enough — the admin tool must also match the
	// caller's scopes (defense in depth: a scoped admin client token
	// shouldn't see management tools outside its granted scope).
	s, _, _ := newTestServer(t, "master-token-xxxxxxxxxxxxxxxxxxxx")

	tools, err := s.listTools(context.Background(), "admin", []string{"admin_add_connection"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, tool := range tools {
		if tool.Name == "admin_add_endpoint" || tool.Name == "admin_register_vault_secret" {
			t.Errorf("expected admin tool %q outside granted scope to be hidden, got %+v", tool.Name, tools)
		}
	}
	found := false
	for _, tool := range tools {
		if tool.Name == "admin_add_connection" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected in-scope admin tool admin_add_connection to be visible")
	}
}

// --- handleRequest: tools/list dispatch --------------------------------------

func TestHandleRequest_ToolsListDispatch(t *testing.T) {
	s, store, _ := newTestServer(t, "master-token-xxxxxxxxxxxxxxxxxxxx")
	conn := store.SeedConnection(&storage.APIConnection{ID: "conn-1", Name: "weather", BaseURL: "https://weather.example.com", AuthType: "none", Enabled: true})
	store.SeedEndpoint(&storage.APIEndpoint{ID: "ep-1", ConnectionID: conn.ID, ToolName: "get_forecast", ToolDescription: "d", Path: "/f", Method: "GET"})

	req := &JSONRPCRequest{JSONRPC: "2.0", Method: "tools/list", ID: 1}
	resp := s.handleRequest(context.Background(), "svc-a", "user", []string{"get_forecast"}, req)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	listResp, ok := resp.Result.(ListToolsResponse)
	if !ok {
		t.Fatalf("unexpected result type %T", resp.Result)
	}
	if len(listResp.Tools) != 1 || listResp.Tools[0].Name != "get_forecast" {
		t.Fatalf("expected exactly get_forecast in tools/list result, got %+v", listResp.Tools)
	}
}

// --- ServeMessages: session re-auth ("anti-hijack") --------------------------
//
// Unlike server_test.go's TestSessionTokenHash_* (which exercise only the
// underlying hash-compare primitive), these drive the real ServeMessages
// HTTP handler end to end: a session is registered directly in s.sessions
// (white-box, same package) with a known TokenHash, and a POST is made
// against it via httptest — proving the handler itself rejects a session-id
// without a body, a mismatched bearer token, and an unknown session id.

func newFakeSession(id, boundToken, identity, role string, scopes []string) *Session {
	rec := httptest.NewRecorder()
	return &Session{
		ID:             id,
		SSEWriter:      rec,
		Flusher:        rec,
		Ctx:            context.Background(),
		ClientIdentity: identity,
		ClientRole:     role,
		Scopes:         scopes,
		TokenHash:      sessionTokenHash(boundToken),
	}
}

func TestServeMessages_UnknownSessionRejected(t *testing.T) {
	s, _, _ := newTestServer(t, "master-token-xxxxxxxxxxxxxxxxxxxx")

	req := httptest.NewRequest(http.MethodPost, "/messages?sessionId=does-not-exist", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	s.ServeMessages(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("got status %d, want %d for unknown sessionId", rec.Code, http.StatusBadRequest)
	}
}

func TestServeMessages_MismatchedTokenRejected(t *testing.T) {
	s, _, _ := newTestServer(t, "master-token-xxxxxxxxxxxxxxxxxxxx")
	sess := newFakeSession("sess-1", "session-owner-token", "master", "admin", []string{"*"})
	s.mu.Lock()
	s.sessions[sess.ID] = sess
	s.mu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/messages?sessionId=sess-1", strings.NewReader(`{"jsonrpc":"2.0","method":"tools/list","id":1}`))
	req.Header.Set("Authorization", "Bearer attacker-token")
	rec := httptest.NewRecorder()
	s.ServeMessages(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want %d for a token that does not match the session", rec.Code, http.StatusUnauthorized)
	}
	// The hijacked session must still exist afterwards — a failed re-auth
	// should not evict the legitimate owner's session.
	s.mu.RLock()
	_, stillActive := s.sessions[sess.ID]
	s.mu.RUnlock()
	if !stillActive {
		t.Errorf("expected session to remain active after a rejected hijack attempt")
	}
}

func TestServeMessages_MatchingTokenDispatches(t *testing.T) {
	s, _, _ := newTestServer(t, "master-token-xxxxxxxxxxxxxxxxxxxx")
	sess := newFakeSession("sess-2", "session-owner-token", "master", "admin", []string{"*"})
	s.mu.Lock()
	s.sessions[sess.ID] = sess
	s.mu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/messages?sessionId=sess-2", strings.NewReader(`{"jsonrpc":"2.0","method":"tools/list","id":1}`))
	req.Header.Set("Authorization", "Bearer session-owner-token")
	rec := httptest.NewRecorder()
	s.ServeMessages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d for a correctly re-authenticated POST; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp JSONRPCResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response body: %v; body=%s", err, rec.Body.String())
	}
	if resp.Error != nil {
		t.Errorf("unexpected JSON-RPC error: %+v", resp.Error)
	}
}

func TestServeMessages_NotificationGetsNoBodyResponse(t *testing.T) {
	// A request with no "id" is a notification: ServeMessages must return
	// 202 Accepted with an empty body, never dispatching to handleRequest.
	s, _, _ := newTestServer(t, "master-token-xxxxxxxxxxxxxxxxxxxx")
	sess := newFakeSession("sess-3", "session-owner-token", "master", "admin", []string{"*"})
	s.mu.Lock()
	s.sessions[sess.ID] = sess
	s.mu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/messages?sessionId=sess-3", strings.NewReader(`{"jsonrpc":"2.0","method":"tools/list"}`))
	req.Header.Set("Authorization", "Bearer session-owner-token")
	rec := httptest.NewRecorder()
	s.ServeMessages(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Errorf("got status %d, want %d for a notification (no id)", rec.Code, http.StatusAccepted)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("expected empty body for a notification, got %q", rec.Body.String())
	}
}

func TestServeMessages_MethodNotAllowed(t *testing.T) {
	s, _, _ := newTestServer(t, "master-token-xxxxxxxxxxxxxxxxxxxx")

	req := httptest.NewRequest(http.MethodGet, "/messages?sessionId=anything", nil)
	rec := httptest.NewRecorder()
	s.ServeMessages(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("got status %d, want %d for a non-POST request", rec.Code, http.StatusMethodNotAllowed)
	}
}
