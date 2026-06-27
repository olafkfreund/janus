package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/calitti/mcp-api-gateway/pkg/storage"
)

type MockVault struct{}

func (m *MockVault) GetSecret(ctx context.Context, secretName string) (string, error) {
	if secretName == "test-key" {
		return "super-secret-api-token", nil
	}
	return "", nil
}

func (m *MockVault) SetSecret(ctx context.Context, secretName string, secretValue string) error {
	return nil
}

func (m *MockVault) ListSecrets(ctx context.Context) ([]string, error) {
	return []string{"test-key"}, nil
}

func (m *MockVault) DeleteSecret(ctx context.Context, secretName string) error {
	return nil
}

func TestExecuteCall_GET(t *testing.T) {
	// Start a mock server to check parameters
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/users/123" {
			t.Errorf("expected path /v1/users/123, got %q", r.URL.Path)
		}
		if r.URL.Query().Get("filter") != "active" {
			t.Errorf("expected query filter=active, got %q", r.URL.Query().Get("filter"))
		}
		if r.Header.Get("Authorization") != "Bearer super-secret-api-token" {
			t.Errorf("expected auth header Bearer super-secret-api-token, got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := NewGatewayClient(&MockVault{})
	conn := &storage.APIConnection{
		BaseURL:       server.URL,
		AuthType:      "bearer",
		AuthSecretRef: "test-key",
		Enabled:       true,
	}

	ep := &storage.APIEndpoint{
		Path:   "/v1/users/{{user_id}}",
		Method: "GET",
	}

	params := map[string]interface{}{
		"user_id": 123,
		"filter":  "active",
	}

	resp, err := client.ExecuteCall(context.Background(), conn, ep, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var data map[string]string
	if err := json.Unmarshal([]byte(resp), &data); err != nil {
		t.Fatalf("failed to parse json response: %v", err)
	}

	if data["status"] != "ok" {
		t.Errorf("expected status ok, got %q", data["status"])
	}
}

func TestExecuteCall_POST_JSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqData map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqData); err != nil {
			t.Fatalf("failed to parse body: %v", err)
		}
		if reqData["id"] != "inv-999" {
			t.Errorf("expected id inv-999, got %v", reqData["id"])
		}
		if reqData["amount"] != float64(150) {
			t.Errorf("expected amount 150, got %v", reqData["amount"])
		}
		w.Write([]byte(`{"result":"created"}`))
	}))
	defer server.Close()

	client := NewGatewayClient(&MockVault{})
	conn := &storage.APIConnection{
		BaseURL:  server.URL,
		AuthType: "none",
		Enabled:  true,
	}

	ep := &storage.APIEndpoint{
		Path:     "/v1/invoices",
		Method:   "POST",
		Template: `{"id": "{{invoice_id}}", "amount": {{amount}} }`,
	}

	params := map[string]interface{}{
		"invoice_id": "inv-999",
		"amount":     150,
	}

	resp, err := client.ExecuteCall(context.Background(), conn, ep, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "{\n  \"result\": \"created\"\n}"
	if resp != expected {
		t.Errorf("expected result body %q, got %q", expected, resp)
	}
}
