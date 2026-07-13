package portal

// Tests for verifyIDToken — the OIDC ID-token verification crux: signature
// validation against a JWKS fetched over OIDC discovery, plus the standard
// claim checks (iss/aud/exp) and the alg-confusion / nonce defenses.
//
// A self-generated RSA keypair signs test tokens; an httptest.Server (TLS,
// since fetchJWKS requires https://) serves /.well-known/openid-configuration
// and a JWKS document. verifyIDToken has no injectable HTTP client, so for
// the duration of this test http.DefaultTransport is swapped for one that
// trusts the test server's self-signed certificate, and restored on cleanup.
// Tests here run sequentially (no t.Parallel) so that swap is safe.

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

	"github.com/calitti/mcp-api-gateway/pkg/config"
	"github.com/golang-jwt/jwt/v5"
)

// oidcTestServer stands up an HTTPS server publishing OIDC discovery and a
// JWKS document for the given RSA public key under the given kid.
func oidcTestServer(t *testing.T, kid string, pub *rsa.PublicKey) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	server := httptest.NewTLSServer(mux)
	t.Cleanup(server.Close)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
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

// trustTestServer points http.DefaultTransport (which oidcHTTPClient's
// zero-value *http.Client resolves to) at the test server's certificate for
// the life of the test.
func trustTestServer(t *testing.T, server *httptest.Server) {
	t.Helper()
	orig := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = orig })
}

func signRS256(t *testing.T, key *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	signed, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("sign RS256 token: %v", err)
	}
	return signed
}

func signHS256(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString([]byte("shared-secret-at-least-32-bytes-long!!"))
	if err != nil {
		t.Fatalf("sign HS256 token: %v", err)
	}
	return signed
}

func signNoneAlg(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	signed, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("sign none-alg token: %v", err)
	}
	return signed
}

// tamperSignature flips the last character of a JWT's signature segment so
// the token still parses (three dot-separated base64url segments) but its
// signature no longer verifies.
func tamperSignature(t *testing.T, tok string) string {
	t.Helper()
	i := len(tok) - 1
	if tok[i] == 'A' {
		return tok[:i] + "B"
	}
	return tok[:i] + "A"
}

func TestVerifyIDToken(t *testing.T) {
	const kid = "test-key"
	const clientID = "gateway-client"

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	server := oidcTestServer(t, kid, &key.PublicKey)
	trustTestServer(t, server)

	p := &PortalServer{config: &config.Config{OIDCIssuer: server.URL, OIDCClientID: clientID}}

	validClaims := func() jwt.MapClaims {
		return jwt.MapClaims{
			"iss": server.URL,
			"aud": clientID,
			"exp": time.Now().Add(time.Hour).Unix(),
		}
	}

	t.Run("valid token is accepted", func(t *testing.T) {
		tok := signRS256(t, key, kid, validClaims())
		claims, err := p.verifyIDToken(context.Background(), tok, "")
		if err != nil {
			t.Fatalf("verifyIDToken() error = %v", err)
		}
		if claims["aud"] != clientID {
			t.Errorf("claims[aud] = %v, want %v", claims["aud"], clientID)
		}
	})

	t.Run("wrong issuer is rejected", func(t *testing.T) {
		c := validClaims()
		c["iss"] = "https://evil.example.com"
		tok := signRS256(t, key, kid, c)
		if _, err := p.verifyIDToken(context.Background(), tok, ""); err == nil {
			t.Fatal("verifyIDToken() with wrong issuer: want error, got nil")
		}
	})

	t.Run("wrong audience is rejected", func(t *testing.T) {
		c := validClaims()
		c["aud"] = "some-other-client"
		tok := signRS256(t, key, kid, c)
		if _, err := p.verifyIDToken(context.Background(), tok, ""); err == nil {
			t.Fatal("verifyIDToken() with wrong audience: want error, got nil")
		}
	})

	t.Run("expired token is rejected", func(t *testing.T) {
		c := validClaims()
		c["exp"] = time.Now().Add(-time.Hour).Unix()
		tok := signRS256(t, key, kid, c)
		if _, err := p.verifyIDToken(context.Background(), tok, ""); err == nil {
			t.Fatal("verifyIDToken() with expired token: want error, got nil")
		}
	})

	t.Run("missing exp is rejected", func(t *testing.T) {
		c := validClaims()
		delete(c, "exp")
		tok := signRS256(t, key, kid, c)
		if _, err := p.verifyIDToken(context.Background(), tok, ""); err == nil {
			t.Fatal("verifyIDToken() with missing exp: want error, got nil")
		}
	})

	t.Run("tampered signature is rejected", func(t *testing.T) {
		tok := tamperSignature(t, signRS256(t, key, kid, validClaims()))
		if _, err := p.verifyIDToken(context.Background(), tok, ""); err == nil {
			t.Fatal("verifyIDToken() with tampered signature: want error, got nil")
		}
	})

	t.Run("alg=none is rejected", func(t *testing.T) {
		tok := signNoneAlg(t, validClaims())
		if _, err := p.verifyIDToken(context.Background(), tok, ""); err == nil {
			t.Fatal("verifyIDToken() with alg=none: want error, got nil")
		}
	})

	t.Run("alg=HS256 is rejected", func(t *testing.T) {
		// Signed with an arbitrary shared secret (an attacker would use the
		// issuer's RSA public key/modulus as the HMAC key in a real
		// alg-confusion attack); either way this must never verify.
		tok := signHS256(t, validClaims())
		if _, err := p.verifyIDToken(context.Background(), tok, ""); err == nil {
			t.Fatal("verifyIDToken() with alg=HS256: want error, got nil")
		}
	})

	t.Run("nonce mismatch is rejected when a nonce is expected", func(t *testing.T) {
		c := validClaims()
		c["nonce"] = "actual-nonce"
		tok := signRS256(t, key, kid, c)
		if _, err := p.verifyIDToken(context.Background(), tok, "expected-nonce"); err == nil {
			t.Fatal("verifyIDToken() with nonce mismatch: want error, got nil")
		}
	})

	t.Run("missing nonce is rejected when a nonce is expected", func(t *testing.T) {
		tok := signRS256(t, key, kid, validClaims())
		if _, err := p.verifyIDToken(context.Background(), tok, "expected-nonce"); err == nil {
			t.Fatal("verifyIDToken() with missing nonce: want error, got nil")
		}
	})

	t.Run("matching nonce is accepted", func(t *testing.T) {
		c := validClaims()
		c["nonce"] = "matching-nonce"
		tok := signRS256(t, key, kid, c)
		if _, err := p.verifyIDToken(context.Background(), tok, "matching-nonce"); err != nil {
			t.Fatalf("verifyIDToken() with matching nonce: error = %v", err)
		}
	})
}
