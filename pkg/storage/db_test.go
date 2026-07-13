package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// --- d.query: '?' -> '$n' placeholder rewrite (Postgres) / passthrough (SQLite) ---

func TestDB_query_placeholderRewrite(t *testing.T) {
	tests := []struct {
		name   string
		driver string
		in     string
		want   string
	}{
		{
			name:   "sqlite: zero placeholders left untouched",
			driver: "sqlite3",
			in:     "SELECT id, name FROM api_connections",
			want:   "SELECT id, name FROM api_connections",
		},
		{
			name:   "sqlite: one placeholder left as '?'",
			driver: "sqlite3",
			in:     "SELECT id FROM api_connections WHERE id = ?",
			want:   "SELECT id FROM api_connections WHERE id = ?",
		},
		{
			name:   "sqlite: many placeholders left as '?'",
			driver: "sqlite3",
			in:     "INSERT INTO t (a, b, c, d, e) VALUES (?, ?, ?, ?, ?)",
			want:   "INSERT INTO t (a, b, c, d, e) VALUES (?, ?, ?, ?, ?)",
		},
		{
			name:   "postgres: zero placeholders left untouched",
			driver: "postgres",
			in:     "SELECT id, name FROM api_connections",
			want:   "SELECT id, name FROM api_connections",
		},
		{
			name:   "postgres: one placeholder becomes $1",
			driver: "postgres",
			in:     "SELECT id FROM api_connections WHERE id = ?",
			want:   "SELECT id FROM api_connections WHERE id = $1",
		},
		{
			name:   "postgres: many placeholders become $1..$n in order",
			driver: "postgres",
			in:     "INSERT INTO t (a, b, c, d, e) VALUES (?, ?, ?, ?, ?)",
			want:   "INSERT INTO t (a, b, c, d, e) VALUES ($1, $2, $3, $4, $5)",
		},
		{
			name:   "postgres: placeholders interleaved with other text",
			driver: "postgres",
			in:     "UPDATE t SET a = ?, b = ? WHERE id = ? AND enabled = ?",
			want:   "UPDATE t SET a = $1, b = $2 WHERE id = $3 AND enabled = $4",
		},
		{
			name:   "unrecognized driver behaves like sqlite (passthrough)",
			driver: "",
			in:     "SELECT * FROM t WHERE id = ?",
			want:   "SELECT * FROM t WHERE id = ?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// d.query only reads the unexported driver field; it needs no
			// live *sql.DB, so a zero-value DB with driver set is sufficient
			// to exercise the rewrite logic in isolation.
			d := &DB{driver: tt.driver}
			got := d.query(tt.in)
			if got != tt.want {
				t.Errorf("query(%q) with driver=%q = %q, want %q", tt.in, tt.driver, got, tt.want)
			}
		})
	}
}

// --- HashToken ---

func TestHashToken(t *testing.T) {
	t.Run("deterministic for same input", func(t *testing.T) {
		got1 := HashToken("secret-token-123")
		got2 := HashToken("secret-token-123")
		if got1 != got2 {
			t.Errorf("HashToken not deterministic: %q != %q", got1, got2)
		}
	})

	t.Run("matches raw sha256 hex encoding", func(t *testing.T) {
		token := "another-token-value"
		sum := sha256.Sum256([]byte(token))
		want := hex.EncodeToString(sum[:])
		got := HashToken(token)
		if got != want {
			t.Errorf("HashToken(%q) = %q, want %q", token, got, want)
		}
	})

	t.Run("output is 64-char lowercase hex (sha256)", func(t *testing.T) {
		got := HashToken("some-token")
		if len(got) != 64 {
			t.Fatalf("expected 64-char hex digest, got length %d (%q)", len(got), got)
		}
		if _, err := hex.DecodeString(got); err != nil {
			t.Errorf("HashToken output is not valid hex: %v", err)
		}
	})

	t.Run("different inputs produce different hashes", func(t *testing.T) {
		a := HashToken("token-a")
		b := HashToken("token-b")
		if a == b {
			t.Errorf("expected different hashes for different inputs, both = %q", a)
		}
	})

	t.Run("empty string hashes deterministically too", func(t *testing.T) {
		got1 := HashToken("")
		got2 := HashToken("")
		if got1 != got2 {
			t.Errorf("HashToken(\"\") not deterministic")
		}
		if got1 == HashToken("nonempty") {
			t.Errorf("HashToken(\"\") collided with HashToken(\"nonempty\")")
		}
	})
}

// --- test DB helper ---

func newTestDB(t *testing.T) *DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	d, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB() error = %v", err)
	}
	t.Cleanup(func() {
		_ = d.Close()
	})
	return d
}

// --- GetClientToken: disabled tokens, hash-based lookup ---

func TestGetClientToken_disabledIsNotValid(t *testing.T) {
	d := newTestDB(t)
	ctx := context.Background()

	plaintext := "plaintext-client-token-abc"
	tok := &ClientToken{
		Token:      plaintext,
		ClientName: "disabled-client",
		ClientRole: "developer",
		Scopes:     "weather_*",
		Enabled:    true,
	}
	if err := d.SaveClientToken(ctx, tok); err != nil {
		t.Fatalf("SaveClientToken(enabled) error = %v", err)
	}

	// Sanity: while enabled, the token is retrievable and reports Enabled=true.
	got, err := d.GetClientToken(ctx, plaintext)
	if err != nil {
		t.Fatalf("GetClientToken (enabled) error = %v", err)
	}
	if !got.Enabled {
		t.Fatalf("expected Enabled=true right after saving an enabled token")
	}

	// Flip to disabled/revoked by re-saving with Enabled=false.
	tok.Enabled = false
	if err := d.SaveClientToken(ctx, tok); err != nil {
		t.Fatalf("SaveClientToken(disabled) error = %v", err)
	}

	got, err = d.GetClientToken(ctx, plaintext)
	if err != nil {
		t.Fatalf("GetClientToken (disabled) error = %v", err)
	}
	if got.Enabled {
		t.Errorf("revoked token must not be reported as Enabled/valid, got Enabled=true")
	}
	if got.ClientName != "disabled-client" {
		t.Errorf("ClientName = %q, want %q", got.ClientName, "disabled-client")
	}
	// The plaintext token must never be echoed back to the caller.
	if got.Token != "" {
		t.Errorf("expected Token to be scrubbed on read, got %q", got.Token)
	}
}

func TestGetClientToken_lookupIsByHashNotPlaintext(t *testing.T) {
	d := newTestDB(t)
	ctx := context.Background()

	plaintext := "another-plaintext-token"
	tok := &ClientToken{
		Token:      plaintext,
		ClientName: "hash-lookup-client",
		ClientRole: "admin",
		Scopes:     "*",
		Enabled:    true,
	}
	if err := d.SaveClientToken(ctx, tok); err != nil {
		t.Fatalf("SaveClientToken() error = %v", err)
	}

	// Looking up by the correct plaintext (which GetClientToken hashes
	// internally) must succeed.
	if _, err := d.GetClientToken(ctx, plaintext); err != nil {
		t.Fatalf("GetClientToken(plaintext) error = %v, want success", err)
	}

	// The row stored in client_tokens must hold the hash, not the plaintext:
	// querying directly for a row whose primary key equals the plaintext
	// value must find nothing.
	row := d.QueryRowContext(ctx, d.query("SELECT client_name FROM client_tokens WHERE token = ?"), plaintext)
	var name string
	if err := row.Scan(&name); err == nil {
		t.Fatalf("expected no row keyed by plaintext token, found client_name=%q", name)
	}

	// Conversely, the row keyed by HashToken(plaintext) must exist and match.
	row = d.QueryRowContext(ctx, d.query("SELECT client_name FROM client_tokens WHERE token = ?"), HashToken(plaintext))
	if err := row.Scan(&name); err != nil {
		t.Fatalf("expected row keyed by HashToken(plaintext), got error: %v", err)
	}
	if name != "hash-lookup-client" {
		t.Errorf("client_name = %q, want %q", name, "hash-lookup-client")
	}

	// A lookup using a wrong plaintext must not resolve to the same token.
	if _, err := d.GetClientToken(ctx, "wrong-token-value"); err == nil {
		t.Errorf("GetClientToken with wrong plaintext unexpectedly succeeded")
	}
}

// --- SaveEndpoint: DefinitionHash + Version bookkeeping ---

func TestSaveEndpoint_hashAndVersion(t *testing.T) {
	d := newTestDB(t)
	ctx := context.Background()

	conn := &APIConnection{
		ID:            "conn-1",
		Name:          "weather-api",
		Description:   "Weather API",
		BaseURL:       "https://weather.example.com",
		AuthType:      "none",
		AuthSecretRef: "",
		Enabled:       true,
		ToolPrefix:    "weather_",
	}
	if err := d.SaveConnection(ctx, conn); err != nil {
		t.Fatalf("SaveConnection() error = %v", err)
	}

	ep := &APIEndpoint{
		ID:               "ep-1",
		ConnectionID:     conn.ID,
		ToolName:         "weather_get_forecast",
		ToolDescription:  "Get the forecast",
		Path:             "/forecast",
		Method:           "GET",
		ParametersSchema: `{"type":"object","properties":{"city":{"type":"string"}}}`,
		Template:         "",
	}

	// First save: version starts at 1, hash is populated.
	if err := d.SaveEndpoint(ctx, ep); err != nil {
		t.Fatalf("SaveEndpoint() (create) error = %v", err)
	}
	if ep.DefinitionHash == "" {
		t.Fatalf("expected non-empty DefinitionHash after first save")
	}
	if ep.Version != 1 {
		t.Fatalf("Version = %d, want 1 on initial save", ep.Version)
	}
	firstHash := ep.DefinitionHash

	// Re-save with an unchanged definition: hash and version must stay the same.
	unchanged := &APIEndpoint{
		ID:               ep.ID,
		ConnectionID:     conn.ID,
		ToolName:         "weather_get_forecast",
		ToolDescription:  "Get the forecast",
		Path:             "/forecast",
		Method:           "GET",
		ParametersSchema: `{"type":"object","properties":{"city":{"type":"string"}}}`,
		Template:         "",
	}
	if err := d.SaveEndpoint(ctx, unchanged); err != nil {
		t.Fatalf("SaveEndpoint() (unchanged) error = %v", err)
	}
	if unchanged.DefinitionHash != firstHash {
		t.Errorf("DefinitionHash changed on unchanged re-save: %q != %q", unchanged.DefinitionHash, firstHash)
	}
	if unchanged.Version != 1 {
		t.Errorf("Version = %d, want unchanged 1 after unchanged re-save", unchanged.Version)
	}

	// Re-save with a changed description: hash must change and version must bump.
	changed := &APIEndpoint{
		ID:               ep.ID,
		ConnectionID:     conn.ID,
		ToolName:         "weather_get_forecast",
		ToolDescription:  "Get the 7-day forecast (updated)",
		Path:             "/forecast",
		Method:           "GET",
		ParametersSchema: `{"type":"object","properties":{"city":{"type":"string"}}}`,
		Template:         "",
	}
	if err := d.SaveEndpoint(ctx, changed); err != nil {
		t.Fatalf("SaveEndpoint() (changed description) error = %v", err)
	}
	if changed.DefinitionHash == firstHash {
		t.Errorf("expected DefinitionHash to change when description changed")
	}
	if changed.Version != 2 {
		t.Errorf("Version = %d, want 2 after a definition change", changed.Version)
	}

	// Re-save again with a changed parameters schema: version bumps again.
	changedSchema := &APIEndpoint{
		ID:               ep.ID,
		ConnectionID:     conn.ID,
		ToolName:         "weather_get_forecast",
		ToolDescription:  "Get the 7-day forecast (updated)",
		Path:             "/forecast",
		Method:           "GET",
		ParametersSchema: `{"type":"object","properties":{"city":{"type":"string"},"days":{"type":"integer"}}}`,
		Template:         "",
	}
	if err := d.SaveEndpoint(ctx, changedSchema); err != nil {
		t.Fatalf("SaveEndpoint() (changed schema) error = %v", err)
	}
	if changedSchema.DefinitionHash == changed.DefinitionHash {
		t.Errorf("expected DefinitionHash to change when parameters_schema changed")
	}
	if changedSchema.Version != 3 {
		t.Errorf("Version = %d, want 3 after a second definition change", changedSchema.Version)
	}
}

// --- Round-trips: SaveConnection -> GetConnections, SaveEndpoint -> GetEndpoints/GetAllEndpoints ---

func TestConnection_saveAndGetRoundTrip(t *testing.T) {
	d := newTestDB(t)
	ctx := context.Background()

	want := &APIConnection{
		ID:            "conn-rt-1",
		Name:          "billing-api",
		Description:   "Billing service",
		BaseURL:       "https://billing.example.com",
		AuthType:      "bearer",
		AuthSecretRef: "vault://billing/token",
		Enabled:       true,
		ToolPrefix:    "billing_",
	}
	if err := d.SaveConnection(ctx, want); err != nil {
		t.Fatalf("SaveConnection() error = %v", err)
	}

	conns, err := d.GetConnections(ctx)
	if err != nil {
		t.Fatalf("GetConnections() error = %v", err)
	}

	var got *APIConnection
	for _, c := range conns {
		if c.ID == want.ID {
			got = c
			break
		}
	}
	if got == nil {
		t.Fatalf("saved connection %q not found in GetConnections() result", want.ID)
	}

	if got.Name != want.Name ||
		got.Description != want.Description ||
		got.BaseURL != want.BaseURL ||
		got.AuthType != want.AuthType ||
		got.AuthSecretRef != want.AuthSecretRef ||
		got.Enabled != want.Enabled ||
		got.ToolPrefix != want.ToolPrefix {
		t.Errorf("GetConnections() round-trip mismatch:\n got  = %+v\n want = %+v", got, want)
	}
}

func TestConnection_disabledRoundTrip(t *testing.T) {
	d := newTestDB(t)
	ctx := context.Background()

	conn := &APIConnection{
		ID:       "conn-disabled-1",
		Name:     "disabled-api",
		BaseURL:  "https://disabled.example.com",
		AuthType: "none",
		Enabled:  false,
	}
	if err := d.SaveConnection(ctx, conn); err != nil {
		t.Fatalf("SaveConnection() error = %v", err)
	}

	conns, err := d.GetConnections(ctx)
	if err != nil {
		t.Fatalf("GetConnections() error = %v", err)
	}
	var got *APIConnection
	for _, c := range conns {
		if c.ID == conn.ID {
			got = c
		}
	}
	if got == nil {
		t.Fatalf("saved connection %q not found", conn.ID)
	}
	if got.Enabled {
		t.Errorf("Enabled = true, want false to round-trip correctly")
	}
}

func TestEndpoint_saveAndGetRoundTrip(t *testing.T) {
	d := newTestDB(t)
	ctx := context.Background()

	conn := &APIConnection{
		ID:       "conn-ep-rt",
		Name:     "orders-api",
		BaseURL:  "https://orders.example.com",
		AuthType: "none",
		Enabled:  true,
	}
	if err := d.SaveConnection(ctx, conn); err != nil {
		t.Fatalf("SaveConnection() error = %v", err)
	}

	ep := &APIEndpoint{
		ID:               "ep-rt-1",
		ConnectionID:     conn.ID,
		ToolName:         "orders_list",
		ToolDescription:  "List orders",
		Path:             "/orders",
		Method:           "GET",
		ParametersSchema: `{"type":"object"}`,
		Template:         "{}",
	}
	if err := d.SaveEndpoint(ctx, ep); err != nil {
		t.Fatalf("SaveEndpoint() error = %v", err)
	}

	// GetEndpoints(connID) round-trip.
	eps, err := d.GetEndpoints(ctx, conn.ID)
	if err != nil {
		t.Fatalf("GetEndpoints() error = %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("GetEndpoints() returned %d endpoints, want 1", len(eps))
	}
	gotByConn := eps[0]

	// GetAllEndpoints() round-trip.
	all, err := d.GetAllEndpoints(ctx)
	if err != nil {
		t.Fatalf("GetAllEndpoints() error = %v", err)
	}
	var gotAll *APIEndpoint
	for _, e := range all {
		if e.ID == ep.ID {
			gotAll = e
		}
	}
	if gotAll == nil {
		t.Fatalf("saved endpoint %q not found in GetAllEndpoints() result", ep.ID)
	}

	for name, got := range map[string]*APIEndpoint{"GetEndpoints": gotByConn, "GetAllEndpoints": gotAll} {
		if got.ConnectionID != ep.ConnectionID ||
			got.ToolName != ep.ToolName ||
			got.ToolDescription != ep.ToolDescription ||
			got.Path != ep.Path ||
			got.Method != ep.Method ||
			got.ParametersSchema != ep.ParametersSchema ||
			got.Template != ep.Template {
			t.Errorf("%s() round-trip mismatch:\n got  = %+v\n want = %+v", name, got, ep)
		}
		if got.DefinitionHash == "" {
			t.Errorf("%s(): expected non-empty DefinitionHash to round-trip", name)
		}
		if got.DefinitionHash != ep.DefinitionHash {
			t.Errorf("%s(): DefinitionHash = %q, want %q", name, got.DefinitionHash, ep.DefinitionHash)
		}
		if got.Version != ep.Version {
			t.Errorf("%s(): Version = %d, want %d", name, got.Version, ep.Version)
		}
	}
}

func TestGetAllEndpoints_multipleConnections(t *testing.T) {
	d := newTestDB(t)
	ctx := context.Background()

	connA := &APIConnection{ID: "conn-a", Name: "api-a", BaseURL: "https://a.example.com", AuthType: "none", Enabled: true}
	connB := &APIConnection{ID: "conn-b", Name: "api-b", BaseURL: "https://b.example.com", AuthType: "none", Enabled: true}
	if err := d.SaveConnection(ctx, connA); err != nil {
		t.Fatalf("SaveConnection(A) error = %v", err)
	}
	if err := d.SaveConnection(ctx, connB); err != nil {
		t.Fatalf("SaveConnection(B) error = %v", err)
	}

	epA := &APIEndpoint{ID: "ep-a", ConnectionID: connA.ID, ToolName: "a_tool", ToolDescription: "A tool", Path: "/a", Method: "GET"}
	epB := &APIEndpoint{ID: "ep-b", ConnectionID: connB.ID, ToolName: "b_tool", ToolDescription: "B tool", Path: "/b", Method: "POST"}
	if err := d.SaveEndpoint(ctx, epA); err != nil {
		t.Fatalf("SaveEndpoint(A) error = %v", err)
	}
	if err := d.SaveEndpoint(ctx, epB); err != nil {
		t.Fatalf("SaveEndpoint(B) error = %v", err)
	}

	all, err := d.GetAllEndpoints(ctx)
	if err != nil {
		t.Fatalf("GetAllEndpoints() error = %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("GetAllEndpoints() returned %d endpoints, want 2", len(all))
	}

	epsForA, err := d.GetEndpoints(ctx, connA.ID)
	if err != nil {
		t.Fatalf("GetEndpoints(connA) error = %v", err)
	}
	if len(epsForA) != 1 || epsForA[0].ID != epA.ID {
		t.Errorf("GetEndpoints(connA) = %+v, want only [%q]", epsForA, epA.ID)
	}

	epsForB, err := d.GetEndpoints(ctx, connB.ID)
	if err != nil {
		t.Fatalf("GetEndpoints(connB) error = %v", err)
	}
	if len(epsForB) != 1 || epsForB[0].ID != epB.ID {
		t.Errorf("GetEndpoints(connB) = %+v, want only [%q]", epsForB, epB.ID)
	}
}

// --- GetConnection / DeleteConnection ---

func TestGetConnection_foundAndNotFound(t *testing.T) {
	d := newTestDB(t)
	ctx := context.Background()

	conn := &APIConnection{ID: "conn-single", Name: "single-api", BaseURL: "https://single.example.com", AuthType: "none", Enabled: true, ToolPrefix: "single_"}
	if err := d.SaveConnection(ctx, conn); err != nil {
		t.Fatalf("SaveConnection() error = %v", err)
	}

	got, err := d.GetConnection(ctx, conn.ID)
	if err != nil {
		t.Fatalf("GetConnection() error = %v", err)
	}
	if got.Name != conn.Name || got.ToolPrefix != conn.ToolPrefix {
		t.Errorf("GetConnection() = %+v, want matching %+v", got, conn)
	}

	if _, err := d.GetConnection(ctx, "does-not-exist"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("GetConnection(missing) error = %v, want sql.ErrNoRows", err)
	}
}

func TestDeleteConnection_removesRow(t *testing.T) {
	d := newTestDB(t)
	ctx := context.Background()

	conn := &APIConnection{ID: "conn-del", Name: "del-api", BaseURL: "https://del.example.com", AuthType: "none", Enabled: true}
	if err := d.SaveConnection(ctx, conn); err != nil {
		t.Fatalf("SaveConnection() error = %v", err)
	}
	if _, err := d.GetConnection(ctx, conn.ID); err != nil {
		t.Fatalf("GetConnection() before delete error = %v", err)
	}

	if err := d.DeleteConnection(ctx, conn.ID); err != nil {
		t.Fatalf("DeleteConnection() error = %v", err)
	}

	if _, err := d.GetConnection(ctx, conn.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("GetConnection() after delete error = %v, want sql.ErrNoRows", err)
	}
}

// --- GetEndpointByToolName / DeleteEndpoint ---

func TestGetEndpointByToolName_foundAndNotFound(t *testing.T) {
	d := newTestDB(t)
	ctx := context.Background()

	conn := &APIConnection{ID: "conn-tn", Name: "toolname-api", BaseURL: "https://tn.example.com", AuthType: "none", Enabled: true}
	if err := d.SaveConnection(ctx, conn); err != nil {
		t.Fatalf("SaveConnection() error = %v", err)
	}
	ep := &APIEndpoint{ID: "ep-tn", ConnectionID: conn.ID, ToolName: "tn_get_thing", ToolDescription: "Get thing", Path: "/thing", Method: "GET"}
	if err := d.SaveEndpoint(ctx, ep); err != nil {
		t.Fatalf("SaveEndpoint() error = %v", err)
	}

	got, err := d.GetEndpointByToolName(ctx, "tn_get_thing")
	if err != nil {
		t.Fatalf("GetEndpointByToolName() error = %v", err)
	}
	if got.ID != ep.ID || got.Path != ep.Path {
		t.Errorf("GetEndpointByToolName() = %+v, want matching %+v", got, ep)
	}

	if _, err := d.GetEndpointByToolName(ctx, "no_such_tool"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("GetEndpointByToolName(missing) error = %v, want sql.ErrNoRows", err)
	}
}

func TestDeleteEndpoint_removesRow(t *testing.T) {
	d := newTestDB(t)
	ctx := context.Background()

	conn := &APIConnection{ID: "conn-epdel", Name: "epdel-api", BaseURL: "https://epdel.example.com", AuthType: "none", Enabled: true}
	if err := d.SaveConnection(ctx, conn); err != nil {
		t.Fatalf("SaveConnection() error = %v", err)
	}
	ep := &APIEndpoint{ID: "ep-del", ConnectionID: conn.ID, ToolName: "epdel_tool", ToolDescription: "desc", Path: "/x", Method: "GET"}
	if err := d.SaveEndpoint(ctx, ep); err != nil {
		t.Fatalf("SaveEndpoint() error = %v", err)
	}

	if err := d.DeleteEndpoint(ctx, ep.ID); err != nil {
		t.Fatalf("DeleteEndpoint() error = %v", err)
	}

	all, err := d.GetAllEndpoints(ctx)
	if err != nil {
		t.Fatalf("GetAllEndpoints() error = %v", err)
	}
	for _, e := range all {
		if e.ID == ep.ID {
			t.Fatalf("endpoint %q still present after DeleteEndpoint()", ep.ID)
		}
	}
}

// --- Audit logs ---

func TestLogAudit_and_GetAuditLogs(t *testing.T) {
	d := newTestDB(t)
	ctx := context.Background()

	if err := d.LogAudit(ctx, "audit-1", "client-a", "tool_one", "success", 12, ""); err != nil {
		t.Fatalf("LogAudit(1) error = %v", err)
	}
	// Sleep-free ordering: rely on ORDER BY timestamp DESC plus insertion order;
	// give the second row a distinguishable error message to verify NULL handling
	// is symmetric with population (COALESCE(..., '') round-trips through '').
	if err := d.LogAudit(ctx, "audit-2", "client-b", "tool_two", "error", 34, "boom"); err != nil {
		t.Fatalf("LogAudit(2) error = %v", err)
	}

	logs, err := d.GetAuditLogs(ctx)
	if err != nil {
		t.Fatalf("GetAuditLogs() error = %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("GetAuditLogs() returned %d logs, want 2", len(logs))
	}

	byID := map[string]*AuditLog{}
	for _, l := range logs {
		byID[l.ID] = l
	}

	first, ok := byID["audit-1"]
	if !ok {
		t.Fatalf("audit-1 not found in GetAuditLogs() result")
	}
	if first.ClientIdentity != "client-a" || first.ToolName != "tool_one" || first.Status != "success" || first.DurationMS != 12 {
		t.Errorf("audit-1 = %+v, mismatch", first)
	}
	if first.ErrorMessage != "" {
		t.Errorf("audit-1 ErrorMessage = %q, want empty (NULL coalesced)", first.ErrorMessage)
	}

	second, ok := byID["audit-2"]
	if !ok {
		t.Fatalf("audit-2 not found in GetAuditLogs() result")
	}
	if second.ErrorMessage != "boom" {
		t.Errorf("audit-2 ErrorMessage = %q, want %q", second.ErrorMessage, "boom")
	}
}

// --- DeleteClientToken / DeleteClientTokenByName / GetClientTokens ---

func TestDeleteClientToken_byRawToken(t *testing.T) {
	d := newTestDB(t)
	ctx := context.Background()

	plaintext := "raw-token-to-delete"
	tok := &ClientToken{Token: plaintext, ClientName: "delete-me", ClientRole: "developer", Scopes: "*", Enabled: true}
	if err := d.SaveClientToken(ctx, tok); err != nil {
		t.Fatalf("SaveClientToken() error = %v", err)
	}
	if _, err := d.GetClientToken(ctx, plaintext); err != nil {
		t.Fatalf("GetClientToken() before delete error = %v", err)
	}

	if err := d.DeleteClientToken(ctx, plaintext); err != nil {
		t.Fatalf("DeleteClientToken() error = %v", err)
	}

	if _, err := d.GetClientToken(ctx, plaintext); err == nil {
		t.Errorf("GetClientToken() after DeleteClientToken() unexpectedly succeeded")
	}
}

func TestDeleteClientTokenByName_revokesRegardlessOfPlaintext(t *testing.T) {
	d := newTestDB(t)
	ctx := context.Background()

	plaintext := "raw-token-by-name"
	tok := &ClientToken{Token: plaintext, ClientName: "named-client", ClientRole: "developer", Scopes: "*", Enabled: true}
	if err := d.SaveClientToken(ctx, tok); err != nil {
		t.Fatalf("SaveClientToken() error = %v", err)
	}

	// Admin path: revoke by name alone, without ever knowing the plaintext.
	if err := d.DeleteClientTokenByName(ctx, "named-client"); err != nil {
		t.Fatalf("DeleteClientTokenByName() error = %v", err)
	}

	if _, err := d.GetClientToken(ctx, plaintext); err == nil {
		t.Errorf("GetClientToken() after DeleteClientTokenByName() unexpectedly succeeded")
	}
}

func TestGetClientTokens_listsAndScrubsTokenField(t *testing.T) {
	d := newTestDB(t)
	ctx := context.Background()

	tokA := &ClientToken{Token: "plain-a", ClientName: "client-a", ClientRole: "admin", Scopes: "*", Enabled: true}
	tokB := &ClientToken{Token: "plain-b", ClientName: "client-b", ClientRole: "developer", Scopes: "weather_*", Enabled: false}
	if err := d.SaveClientToken(ctx, tokA); err != nil {
		t.Fatalf("SaveClientToken(A) error = %v", err)
	}
	if err := d.SaveClientToken(ctx, tokB); err != nil {
		t.Fatalf("SaveClientToken(B) error = %v", err)
	}

	all, err := d.GetClientTokens(ctx)
	if err != nil {
		t.Fatalf("GetClientTokens() error = %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("GetClientTokens() returned %d tokens, want 2", len(all))
	}

	byName := map[string]*ClientToken{}
	for _, tk := range all {
		byName[tk.ClientName] = tk
		if tk.Token != "" {
			t.Errorf("GetClientTokens(): token %q leaked plaintext/hash %q, want scrubbed", tk.ClientName, tk.Token)
		}
	}
	if !byName["client-a"].Enabled {
		t.Errorf("client-a Enabled = false, want true")
	}
	if byName["client-b"].Enabled {
		t.Errorf("client-b Enabled = true, want false")
	}
	if byName["client-b"].Scopes != "weather_*" {
		t.Errorf("client-b Scopes = %q, want %q", byName["client-b"].Scopes, "weather_*")
	}
}

// --- Config cache: EnableConfigCache + bust-on-write ---

func TestConfigCache_connectionsBustOnWrite(t *testing.T) {
	d := newTestDB(t)
	d.EnableConfigCache(time.Minute)
	ctx := context.Background()

	conn := &APIConnection{ID: "conn-cache-1", Name: "cache-api-1", BaseURL: "https://cache1.example.com", AuthType: "none", Enabled: true}
	if err := d.SaveConnection(ctx, conn); err != nil {
		t.Fatalf("SaveConnection() error = %v", err)
	}

	first, err := d.GetConnections(ctx)
	if err != nil {
		t.Fatalf("GetConnections() (prime cache) error = %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("GetConnections() = %d entries, want 1", len(first))
	}

	// Insert a second connection directly via SaveConnection, which purges
	// the cache; a subsequent read must observe the new row rather than the
	// stale cached slice.
	conn2 := &APIConnection{ID: "conn-cache-2", Name: "cache-api-2", BaseURL: "https://cache2.example.com", AuthType: "none", Enabled: true}
	if err := d.SaveConnection(ctx, conn2); err != nil {
		t.Fatalf("SaveConnection(2) error = %v", err)
	}

	second, err := d.GetConnections(ctx)
	if err != nil {
		t.Fatalf("GetConnections() (post-write) error = %v", err)
	}
	if len(second) != 2 {
		t.Fatalf("GetConnections() after write = %d entries, want 2 (cache not busted)", len(second))
	}

	// DeleteConnection must also bust the cache.
	if err := d.DeleteConnection(ctx, conn2.ID); err != nil {
		t.Fatalf("DeleteConnection() error = %v", err)
	}
	third, err := d.GetConnections(ctx)
	if err != nil {
		t.Fatalf("GetConnections() (post-delete) error = %v", err)
	}
	if len(third) != 1 {
		t.Fatalf("GetConnections() after delete = %d entries, want 1 (cache not busted)", len(third))
	}
}

func TestConfigCache_endpointsBustOnWrite(t *testing.T) {
	d := newTestDB(t)
	d.EnableConfigCache(time.Minute)
	ctx := context.Background()

	conn := &APIConnection{ID: "conn-cache-ep", Name: "cache-ep-api", BaseURL: "https://cacheep.example.com", AuthType: "none", Enabled: true}
	if err := d.SaveConnection(ctx, conn); err != nil {
		t.Fatalf("SaveConnection() error = %v", err)
	}
	ep1 := &APIEndpoint{ID: "ep-cache-1", ConnectionID: conn.ID, ToolName: "cache_tool_1", ToolDescription: "d1", Path: "/1", Method: "GET"}
	if err := d.SaveEndpoint(ctx, ep1); err != nil {
		t.Fatalf("SaveEndpoint(1) error = %v", err)
	}

	first, err := d.GetAllEndpoints(ctx)
	if err != nil {
		t.Fatalf("GetAllEndpoints() (prime cache) error = %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("GetAllEndpoints() = %d entries, want 1", len(first))
	}

	ep2 := &APIEndpoint{ID: "ep-cache-2", ConnectionID: conn.ID, ToolName: "cache_tool_2", ToolDescription: "d2", Path: "/2", Method: "GET"}
	if err := d.SaveEndpoint(ctx, ep2); err != nil {
		t.Fatalf("SaveEndpoint(2) error = %v", err)
	}

	second, err := d.GetAllEndpoints(ctx)
	if err != nil {
		t.Fatalf("GetAllEndpoints() (post-write) error = %v", err)
	}
	if len(second) != 2 {
		t.Fatalf("GetAllEndpoints() after write = %d entries, want 2 (cache not busted)", len(second))
	}

	if err := d.DeleteEndpoint(ctx, ep2.ID); err != nil {
		t.Fatalf("DeleteEndpoint() error = %v", err)
	}
	third, err := d.GetAllEndpoints(ctx)
	if err != nil {
		t.Fatalf("GetAllEndpoints() (post-delete) error = %v", err)
	}
	if len(third) != 1 {
		t.Fatalf("GetAllEndpoints() after delete = %d entries, want 1 (cache not busted)", len(third))
	}
}

func TestConfigCache_disabledByDefault_alwaysHitsDB(t *testing.T) {
	// Without EnableConfigCache, GetConnections must reflect every write
	// immediately since d.connCache is nil (Get/Set on a nil *TTLCache are
	// no-ops per pkg/cache's nil-receiver guard).
	d := newTestDB(t)
	ctx := context.Background()

	conn := &APIConnection{ID: "conn-nocache", Name: "nocache-api", BaseURL: "https://nocache.example.com", AuthType: "none", Enabled: true}
	if err := d.SaveConnection(ctx, conn); err != nil {
		t.Fatalf("SaveConnection() error = %v", err)
	}
	conns, err := d.GetConnections(ctx)
	if err != nil {
		t.Fatalf("GetConnections() error = %v", err)
	}
	if len(conns) != 1 {
		t.Fatalf("GetConnections() = %d entries, want 1", len(conns))
	}
}

// --- TunePool ---

func TestTunePool_appliesNonZeroSettings(t *testing.T) {
	d := newTestDB(t)

	// Zero values must be no-ops (left at *sql.DB defaults); non-zero values
	// must be applied. We can't introspect *sql.DB's configured limits
	// directly, so this exercises all branches and asserts the pool still
	// works afterwards (no panic, queries still succeed).
	d.TunePool(0, 0, 0)
	d.TunePool(5, 2, time.Minute)

	ctx := context.Background()
	if _, err := d.GetConnections(ctx); err != nil {
		t.Fatalf("GetConnections() after TunePool() error = %v", err)
	}
}

// --- NewDB reopened on the same path: exercises initSchema's ALTER TABLE
// "column already exists" branches, which only fire on a second run against
// a database that already has the columns. ---

func TestNewDB_reopenSamePathRunsSchemaIdempotently(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "reopen.db")

	d1, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB() (first open) error = %v", err)
	}
	ctx := context.Background()
	conn := &APIConnection{ID: "conn-reopen", Name: "reopen-api", BaseURL: "https://reopen.example.com", AuthType: "none", Enabled: true}
	if err := d1.SaveConnection(ctx, conn); err != nil {
		t.Fatalf("SaveConnection() error = %v", err)
	}
	if err := d1.Close(); err != nil {
		t.Fatalf("Close() (first) error = %v", err)
	}

	// Reopening the same file re-runs initSchema, including the ALTER TABLE
	// statements against columns that already exist; NewDB must treat that
	// as success rather than propagating the driver's "duplicate column" error.
	d2, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB() (second open / re-init schema) error = %v", err)
	}
	defer d2.Close()

	conns, err := d2.GetConnections(ctx)
	if err != nil {
		t.Fatalf("GetConnections() after reopen error = %v", err)
	}
	if len(conns) != 1 || conns[0].ID != conn.ID {
		t.Errorf("GetConnections() after reopen = %+v, want persisted row %+v", conns, conn)
	}
}
