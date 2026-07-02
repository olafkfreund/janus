package oauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestProtectedResourceMetadata(t *testing.T) {
	t.Run("valid config produces expected shape", func(t *testing.T) {
		cfg := Config{
			Enabled:              true,
			ResourceURI:          "https://gateway.example.com/mcp",
			AuthorizationServers: []string{"https://as.example.com"},
			ScopesSupported:      []string{"mcp:read", "mcp:write"},
		}

		body, err := ProtectedResourceMetadata(cfg)
		if err != nil {
			t.Fatalf("ProtectedResourceMetadata() error = %v", err)
		}

		var got map[string]interface{}
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("unmarshal metadata: %v", err)
		}

		if got["resource"] != cfg.ResourceURI {
			t.Errorf("resource = %v, want %v", got["resource"], cfg.ResourceURI)
		}
		authServers, ok := got["authorization_servers"].([]interface{})
		if !ok || len(authServers) != 1 || authServers[0] != "https://as.example.com" {
			t.Errorf("authorization_servers = %v, want [https://as.example.com]", got["authorization_servers"])
		}
		scopes, ok := got["scopes_supported"].([]interface{})
		if !ok || len(scopes) != 2 {
			t.Errorf("scopes_supported = %v, want 2 scopes", got["scopes_supported"])
		}
		bearerMethods, ok := got["bearer_methods_supported"].([]interface{})
		if !ok || len(bearerMethods) != 1 || bearerMethods[0] != "header" {
			t.Errorf("bearer_methods_supported = %v, want [header]", got["bearer_methods_supported"])
		}
	})

	t.Run("missing ResourceURI is an error", func(t *testing.T) {
		if _, err := ProtectedResourceMetadata(Config{}); err == nil {
			t.Fatal("ProtectedResourceMetadata() with empty ResourceURI: want error, got nil")
		}
	})

	t.Run("nil AuthorizationServers marshals to empty array not null", func(t *testing.T) {
		body, err := ProtectedResourceMetadata(Config{ResourceURI: "https://gateway.example.com/mcp"})
		if err != nil {
			t.Fatalf("ProtectedResourceMetadata() error = %v", err)
		}
		var got map[string]interface{}
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("unmarshal metadata: %v", err)
		}
		authServers, ok := got["authorization_servers"].([]interface{})
		if !ok || len(authServers) != 0 {
			t.Errorf("authorization_servers = %v, want []", got["authorization_servers"])
		}
	})
}

func TestChallengeHeader(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		errCode string
		want    string
	}{
		{
			name:    "with error code",
			cfg:     Config{ResourceURI: "https://gateway.example.com/mcp"},
			errCode: "invalid_token",
			want:    `Bearer resource_metadata="https://gateway.example.com/mcp/.well-known/oauth-protected-resource", error="invalid_token"`,
		},
		{
			name:    "trailing slash on resource URI is trimmed",
			cfg:     Config{ResourceURI: "https://gateway.example.com/mcp/"},
			errCode: "invalid_token",
			want:    `Bearer resource_metadata="https://gateway.example.com/mcp/.well-known/oauth-protected-resource", error="invalid_token"`,
		},
		{
			name:    "empty error code omits error param",
			cfg:     Config{ResourceURI: "https://gateway.example.com/mcp"},
			errCode: "",
			want:    `Bearer resource_metadata="https://gateway.example.com/mcp/.well-known/oauth-protected-resource"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ChallengeHeader(tt.cfg, tt.errCode); got != tt.want {
				t.Errorf("ChallengeHeader() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAudienceContains(t *testing.T) {
	const resource = "https://gateway.example.com/mcp"

	tests := []struct {
		name string
		aud  interface{}
		want bool
	}{
		{"string match", resource, true},
		{"string mismatch", "https://other.example.com", false},
		{"array contains", []interface{}{"https://other.example.com", resource}, true},
		{"array does not contain", []interface{}{"https://other.example.com"}, false},
		{"empty array", []interface{}{}, false},
		{"nil aud", nil, false},
		{"[]string contains", []string{"https://other.example.com", resource}, true},
		{"[]string does not contain", []string{"https://other.example.com"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := audienceContains(tt.aud, resource); got != tt.want {
				t.Errorf("audienceContains(%v, %q) = %v, want %v", tt.aud, resource, got, tt.want)
			}
		})
	}

	t.Run("empty resourceURI never matches", func(t *testing.T) {
		if audienceContains(resource, "") {
			t.Error("audienceContains with empty resourceURI: want false, got true")
		}
	})
}

func TestParseScopes(t *testing.T) {
	tests := []struct {
		name string
		raw  map[string]any
		want []string
	}{
		{"scope string", map[string]any{"scope": "mcp:read mcp:write"}, []string{"mcp:read", "mcp:write"}},
		{"scp string fallback", map[string]any{"scp": "mcp:read mcp:write"}, []string{"mcp:read", "mcp:write"}},
		{"scp array fallback", map[string]any{"scp": []interface{}{"mcp:read", "mcp:write"}}, []string{"mcp:read", "mcp:write"}},
		{"scope takes precedence over scp", map[string]any{"scope": "a", "scp": "b"}, []string{"a"}},
		{"neither present", map[string]any{}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseScopes(tt.raw)
			if len(got) != len(tt.want) {
				t.Fatalf("parseScopes() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseScopes()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestNewValidator(t *testing.T) {
	t.Run("missing ResourceURI is an error", func(t *testing.T) {
		if _, err := NewValidator(Config{}); err == nil {
			t.Fatal("NewValidator() with empty ResourceURI: want error, got nil")
		}
	})

	t.Run("valid config succeeds", func(t *testing.T) {
		v, err := NewValidator(Config{ResourceURI: "https://gateway.example.com/mcp"})
		if err != nil {
			t.Fatalf("NewValidator() error = %v", err)
		}
		if v == nil {
			t.Fatal("NewValidator() returned nil Validator with no error")
		}
	})
}

func TestValidateToken_IssuerNotTrusted(t *testing.T) {
	v, err := NewValidator(Config{
		ResourceURI:          "https://gateway.example.com/mcp",
		AuthorizationServers: []string{"https://as.example.com"},
	})
	if err != nil {
		t.Fatalf("NewValidator() error = %v", err)
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}

	tests := []struct {
		name string
		iss  string
	}{
		{"issuer not in allow-list", "https://evil.example.com"},
		{"issuer not https", "http://as.example.com"},
		{"empty issuer", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tok := signToken(t, key, "test-key", jwt.MapClaims{
				"iss": tt.iss,
				"sub": "user-1",
				"aud": "https://gateway.example.com/mcp",
				"exp": time.Now().Add(time.Hour).Unix(),
			})

			if _, err := v.ValidateToken(context.Background(), tok); err == nil {
				t.Fatal("ValidateToken() with untrusted issuer: want error, got nil")
			}
		})
	}
}

// signingTestServer stands up an HTTPS test server that publishes OAuth
// authorization-server metadata and a JWKS document for the given RSA public
// key under the given kid.
func signingTestServer(t *testing.T, kid string, pub *rsa.PublicKey) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	server := httptest.NewTLSServer(mux)
	t.Cleanup(server.Close)

	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":   server.URL,
			"jwks_uri": server.URL + "/jwks.json",
		})
	})

	mux.HandleFunc("/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"keys": []map[string]string{
				{
					"kty": "RSA",
					"kid": kid,
					"use": "sig",
					"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
					"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
				},
			},
		})
	})

	return server
}

// signToken mints an RS256-signed JWT with the given claims and kid header,
// using the private key that corresponds to the public key served by
// signingTestServer.
func signToken(t *testing.T, key *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

// signHS256Token mints an HMAC-signed JWT — used to assert the validator
// rejects non-RSA algorithms.
func signHS256Token(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte("shared-secret-at-least-32-bytes!"))
	if err != nil {
		t.Fatalf("sign HS256 token: %v", err)
	}
	return signed
}

func TestValidateToken_SignatureAndClaims(t *testing.T) {
	const resourceURI = "https://gateway.example.com/mcp"
	const kid = "test-key"

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}

	server := signingTestServer(t, kid, &key.PublicKey)

	newValidator := func(t *testing.T) *Validator {
		t.Helper()
		v, err := NewValidator(Config{
			ResourceURI:          resourceURI,
			AuthorizationServers: []string{server.URL},
		})
		if err != nil {
			t.Fatalf("NewValidator() error = %v", err)
		}
		// Trust the httptest TLS server's self-signed certificate.
		v.httpClient = server.Client()
		return v
	}

	t.Run("valid token with string aud is accepted", func(t *testing.T) {
		v := newValidator(t)
		tok := signToken(t, key, kid, jwt.MapClaims{
			"iss":   server.URL,
			"sub":   "user-1",
			"aud":   resourceURI,
			"scope": "mcp:read mcp:write",
			"exp":   time.Now().Add(time.Hour).Unix(),
		})

		claims, err := v.ValidateToken(context.Background(), tok)
		if err != nil {
			t.Fatalf("ValidateToken() error = %v", err)
		}
		if claims.Subject != "user-1" {
			t.Errorf("Subject = %q, want %q", claims.Subject, "user-1")
		}
		if claims.Issuer != server.URL {
			t.Errorf("Issuer = %q, want %q", claims.Issuer, server.URL)
		}
		if len(claims.Scopes) != 2 || claims.Scopes[0] != "mcp:read" || claims.Scopes[1] != "mcp:write" {
			t.Errorf("Scopes = %v, want [mcp:read mcp:write]", claims.Scopes)
		}
		if claims.Raw == nil || claims.Raw["sub"] != "user-1" {
			t.Errorf("Raw = %v, want to contain sub=user-1", claims.Raw)
		}
	})

	t.Run("valid token with array aud is accepted", func(t *testing.T) {
		v := newValidator(t)
		tok := signToken(t, key, kid, jwt.MapClaims{
			"iss": server.URL,
			"sub": "user-2",
			"aud": []string{"https://other.example.com", resourceURI},
			"exp": time.Now().Add(time.Hour).Unix(),
		})

		if _, err := v.ValidateToken(context.Background(), tok); err != nil {
			t.Fatalf("ValidateToken() error = %v", err)
		}
	})

	t.Run("mismatched aud is rejected", func(t *testing.T) {
		v := newValidator(t)
		tok := signToken(t, key, kid, jwt.MapClaims{
			"iss": server.URL,
			"sub": "user-3",
			"aud": "https://other.example.com",
			"exp": time.Now().Add(time.Hour).Unix(),
		})

		if _, err := v.ValidateToken(context.Background(), tok); err == nil {
			t.Fatal("ValidateToken() with mismatched aud: want error, got nil")
		}
	})

	t.Run("expired token is rejected", func(t *testing.T) {
		v := newValidator(t)
		tok := signToken(t, key, kid, jwt.MapClaims{
			"iss": server.URL,
			"sub": "user-4",
			"aud": resourceURI,
			"exp": time.Now().Add(-time.Hour).Unix(),
		})

		if _, err := v.ValidateToken(context.Background(), tok); err == nil {
			t.Fatal("ValidateToken() with expired token: want error, got nil")
		}
	})

	t.Run("missing exp is rejected", func(t *testing.T) {
		v := newValidator(t)
		tok := signToken(t, key, kid, jwt.MapClaims{
			"iss": server.URL,
			"sub": "user-5",
			"aud": resourceURI,
		})

		if _, err := v.ValidateToken(context.Background(), tok); err == nil {
			t.Fatal("ValidateToken() with missing exp: want error, got nil")
		}
	})

	t.Run("HMAC-signed token is rejected", func(t *testing.T) {
		v := newValidator(t)
		tok := signHS256Token(t, jwt.MapClaims{
			"iss": server.URL,
			"sub": "user-6",
			"aud": resourceURI,
			"exp": time.Now().Add(time.Hour).Unix(),
		})

		if _, err := v.ValidateToken(context.Background(), tok); err == nil {
			t.Fatal("ValidateToken() with HMAC-signed token: want error, got nil")
		}
	})

	t.Run("unknown kid is rejected", func(t *testing.T) {
		v := newValidator(t)
		tok := signToken(t, key, "no-such-kid", jwt.MapClaims{
			"iss": server.URL,
			"sub": "user-7",
			"aud": resourceURI,
			"exp": time.Now().Add(time.Hour).Unix(),
		})

		if _, err := v.ValidateToken(context.Background(), tok); err == nil {
			t.Fatal("ValidateToken() with unknown kid: want error, got nil")
		}
	})
}
