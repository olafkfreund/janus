package oauth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// jwksCacheTTL bounds how long a fetched JWKS document is trusted before a
// fresh fetch is required. Short enough that a rotated/revoked key is picked
// up promptly; long enough to avoid hammering the authorization server.
const jwksCacheTTL = 5 * time.Minute

// jwksCacheEntry holds the RSA signing keys published by an issuer, keyed by
// kid, along with when they were fetched.
type jwksCacheEntry struct {
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time
}

// Validator verifies OAuth 2.1 access tokens presented to this resource
// server. It discovers and caches each trusted authorization server's JWKS
// in-memory.
type Validator struct {
	config     Config
	httpClient *http.Client

	jwksMu    sync.Mutex
	jwksCache map[string]*jwksCacheEntry
}

// NewValidator constructs a Validator for the given resource-server config.
func NewValidator(c Config) (*Validator, error) {
	if c.ResourceURI == "" {
		return nil, fmt.Errorf("oauth: NewValidator: ResourceURI must be set")
	}
	return &Validator{
		config:     c,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		jwksCache:  make(map[string]*jwksCacheEntry),
	}, nil
}

// ValidateToken verifies the access token: signature via the issuer's JWKS
// (discover <issuer>/.well-known/openid-configuration -> jwks_uri, or
// <issuer>/.well-known/oauth-authorization-server), RS256/384/512 only
// (reject none/HMAC), exp required, iss in AuthorizationServers, and aud
// CONTAINS ResourceURI (RFC 8707). Fails closed on any verification error.
func (v *Validator) ValidateToken(ctx context.Context, tokenString string) (*Claims, error) {
	// Peek at the (unverified) issuer claim so we know which authorization
	// server's JWKS to check the signature against. This value is untrusted
	// until the signature is verified below; it is only ever used to select
	// a JWKS source that is itself pinned to the AuthorizationServers
	// allow-list, so it cannot be used to smuggle in an untrusted key set.
	unverified, _, err := jwt.NewParser().ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		return nil, fmt.Errorf("oauth: parse token: %w", err)
	}
	unverifiedClaims, ok := unverified.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("oauth: parse token: unexpected claims type")
	}
	iss, _ := unverifiedClaims["iss"].(string)
	if !v.issuerTrusted(iss) {
		return nil, fmt.Errorf("oauth: issuer %q is not a trusted authorization server", iss)
	}

	keys, err := v.fetchJWKS(ctx, iss)
	if err != nil {
		return nil, fmt.Errorf("oauth: resolve issuer signing keys: %w", err)
	}

	keyfunc := func(token *jwt.Token) (interface{}, error) {
		// Pin to RSA; never accept "none" or HMAC signing algorithms.
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("oauth: unexpected signing method %v", token.Header["alg"])
		}
		kid, _ := token.Header["kid"].(string)
		if kid == "" {
			if len(keys) == 1 {
				for _, k := range keys {
					return k, nil
				}
			}
			return nil, fmt.Errorf("oauth: token missing kid and issuer has multiple keys")
		}
		key, ok := keys[kid]
		if !ok {
			return nil, fmt.Errorf("oauth: no jwks key for kid %q", kid)
		}
		return key, nil
	}

	claims := jwt.MapClaims{}
	if _, err := jwt.ParseWithClaims(tokenString, claims, keyfunc,
		jwt.WithValidMethods([]string{"RS256", "RS384", "RS512"}),
		jwt.WithExpirationRequired(),
		jwt.WithIssuer(iss),
	); err != nil {
		return nil, fmt.Errorf("oauth: verify token: %w", err)
	}

	if !audienceContains(claims["aud"], v.config.ResourceURI) {
		return nil, fmt.Errorf("oauth: token audience does not include resource %q", v.config.ResourceURI)
	}

	sub, _ := claims["sub"].(string)
	return &Claims{
		Subject: sub,
		Scopes:  parseScopes(claims),
		Issuer:  iss,
		Raw:     map[string]any(claims),
	}, nil
}

// issuerTrusted reports whether iss is both an https:// URL and exactly
// matches (ignoring a trailing slash) one of the configured
// AuthorizationServers. Empty or non-https issuers are never trusted.
func (v *Validator) issuerTrusted(iss string) bool {
	if iss == "" || !strings.HasPrefix(iss, "https://") {
		return false
	}
	trimmed := strings.TrimSuffix(iss, "/")
	for _, a := range v.config.AuthorizationServers {
		if strings.TrimSuffix(a, "/") == trimmed {
			return true
		}
	}
	return false
}

// fetchJWKS resolves and returns the issuer's RSA signing keys (by kid),
// discovering the jwks_uri via authorization-server/OIDC metadata and
// caching the result in-memory for a short TTL. The issuer (and the
// discovered jwks_uri) must be https://.
func (v *Validator) fetchJWKS(ctx context.Context, issuer string) (map[string]*rsa.PublicKey, error) {
	issuer = strings.TrimSuffix(issuer, "/")
	if !strings.HasPrefix(issuer, "https://") {
		return nil, fmt.Errorf("issuer must be https, got %q", issuer)
	}

	v.jwksMu.Lock()
	if e, ok := v.jwksCache[issuer]; ok && time.Since(e.fetchedAt) < jwksCacheTTL {
		keys := e.keys
		v.jwksMu.Unlock()
		return keys, nil
	}
	v.jwksMu.Unlock()

	jwksURI, err := v.discoverJWKSURI(ctx, issuer)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(jwksURI, "https://") {
		return nil, fmt.Errorf("jwks_uri must be https, got %q", jwksURI)
	}

	keys, err := v.fetchJWKSKeys(ctx, jwksURI)
	if err != nil {
		return nil, err
	}

	v.jwksMu.Lock()
	v.jwksCache[issuer] = &jwksCacheEntry{keys: keys, fetchedAt: time.Now()}
	v.jwksMu.Unlock()
	return keys, nil
}

// discoverJWKSURI resolves an issuer's jwks_uri from its OAuth
// authorization-server metadata (RFC 8414), falling back to OIDC discovery
// metadata for issuers that only publish that document.
func (v *Validator) discoverJWKSURI(ctx context.Context, issuer string) (string, error) {
	var lastErr error
	for _, wellKnown := range []string{
		"/.well-known/oauth-authorization-server",
		"/.well-known/openid-configuration",
	} {
		uri, err := v.fetchDiscoveryJWKSURI(ctx, issuer+wellKnown)
		if err != nil {
			lastErr = err
			continue
		}
		return uri, nil
	}
	return "", fmt.Errorf("discover jwks_uri for issuer %q: %w", issuer, lastErr)
}

// fetchDiscoveryJWKSURI fetches an authorization-server/OIDC metadata
// document and returns its jwks_uri field.
func (v *Validator) fetchDiscoveryJWKSURI(ctx context.Context, discoveryURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return "", fmt.Errorf("build discovery request: %w", err)
	}
	resp, err := v.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch discovery document: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("discovery document %s returned HTTP %d", discoveryURL, resp.StatusCode)
	}
	var doc struct {
		JWKSURI string `json:"jwks_uri"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&doc); err != nil {
		return "", fmt.Errorf("decode discovery document: %w", err)
	}
	if doc.JWKSURI == "" {
		return "", fmt.Errorf("discovery document missing jwks_uri")
	}
	return doc.JWKSURI, nil
}

// fetchJWKSKeys fetches a JWKS document and parses its RSA signing keys by
// kid.
func (v *Validator) fetchJWKSKeys(ctx context.Context, jwksURI string) (map[string]*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURI, nil)
	if err != nil {
		return nil, fmt.Errorf("build jwks request: %w", err)
	}
	resp, err := v.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch jwks: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jwks endpoint returned HTTP %d", resp.StatusCode)
	}
	var jwks struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			Use string `json:"use"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&jwks); err != nil {
		return nil, fmt.Errorf("decode jwks: %w", err)
	}
	keys := make(map[string]*rsa.PublicKey)
	for _, k := range jwks.Keys {
		if k.Kty != "RSA" {
			continue
		}
		if k.Use != "" && k.Use != "sig" {
			continue
		}
		pub, err := rsaPublicKeyFromJWK(k.N, k.E)
		if err != nil {
			continue
		}
		keys[k.Kid] = pub
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("jwks contained no usable RSA signing keys")
	}
	return keys, nil
}

// rsaPublicKeyFromJWK builds an RSA public key from the base64url-encoded
// modulus (n) and exponent (e) of a JWK.
func rsaPublicKeyFromJWK(nB64, eB64 string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nB64)
	if err != nil {
		return nil, fmt.Errorf("decode jwk modulus: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eB64)
	if err != nil {
		return nil, fmt.Errorf("decode jwk exponent: %w", err)
	}
	e := new(big.Int).SetBytes(eBytes)
	if !e.IsInt64() || e.Int64() < 2 {
		return nil, fmt.Errorf("invalid jwk exponent")
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: int(e.Int64())}, nil
}
