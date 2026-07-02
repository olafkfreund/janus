// Package oauth implements OAuth 2.1 resource-server primitives for the MCP
// gateway: RFC 9728 protected-resource metadata, the RFC 9728 §5.1
// WWW-Authenticate challenge, and RFC 8707 resource-indicator (audience)
// aware access-token validation against a trusted authorization server's
// JWKS.
//
// This package never issues or mints tokens — the gateway is a resource
// server only. It fails closed: any ambiguity in configuration or a token's
// claims results in rejection, never an implicit allow.
package oauth

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Config holds resource-server settings, sourced from gateway config.
type Config struct {
	Enabled bool

	// ResourceURI is the canonical URI identifying THIS MCP resource (RFC
	// 8707 audience). Access tokens presented to the gateway must carry this
	// value in their "aud" claim.
	ResourceURI string

	// AuthorizationServers lists the issuer URLs of trusted authorization
	// servers. Tokens whose "iss" claim does not match one of these
	// (exactly, ignoring a trailing slash) are rejected.
	AuthorizationServers []string

	// ScopesSupported lists the scopes the resource advertises in its
	// protected-resource metadata document.
	ScopesSupported []string
}

// protectedResourceMetadata is the RFC 9728 §3.1 response body shape.
type protectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	ScopesSupported        []string `json:"scopes_supported,omitempty"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
}

// ProtectedResourceMetadata returns the JSON body for
// GET /.well-known/oauth-protected-resource (RFC 9728).
func ProtectedResourceMetadata(c Config) ([]byte, error) {
	if c.ResourceURI == "" {
		return nil, fmt.Errorf("oauth: protected resource metadata: ResourceURI must be set")
	}

	authServers := c.AuthorizationServers
	if authServers == nil {
		authServers = []string{}
	}

	body, err := json.MarshalIndent(protectedResourceMetadata{
		Resource:               c.ResourceURI,
		AuthorizationServers:   authServers,
		ScopesSupported:        c.ScopesSupported,
		BearerMethodsSupported: []string{"header"},
	}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("oauth: marshal protected resource metadata: %w", err)
	}
	return body, nil
}

// ChallengeHeader returns the WWW-Authenticate header VALUE to send on a 401,
// pointing clients at the resource metadata (RFC 9728 §5.1), e.g.:
//
//	Bearer resource_metadata="<ResourceURI>/.well-known/oauth-protected-resource", error="invalid_token"
//
// If errCode is empty, the error parameter is omitted.
func ChallengeHeader(c Config, errCode string) string {
	metadataURL := strings.TrimSuffix(c.ResourceURI, "/") + "/.well-known/oauth-protected-resource"
	if errCode == "" {
		return fmt.Sprintf(`Bearer resource_metadata=%q`, metadataURL)
	}
	return fmt.Sprintf(`Bearer resource_metadata=%q, error=%q`, metadataURL, errCode)
}

// Claims holds the subset of validated access-token claims the gateway acts
// on, alongside the full raw claim set for callers that need more.
type Claims struct {
	Subject string
	Scopes  []string // parsed from "scope" (space-delimited) or "scp"
	Issuer  string
	Raw     map[string]any
}

// parseScopes extracts a scope list from an OAuth "scope" claim
// (space-delimited string, per RFC 6749 §3.3) or, failing that, an "scp"
// claim as used by some authorization servers (either a space-delimited
// string or a JSON array of strings).
func parseScopes(raw map[string]any) []string {
	if s, ok := raw["scope"].(string); ok && s != "" {
		return strings.Fields(s)
	}
	switch v := raw["scp"].(type) {
	case string:
		if v != "" {
			return strings.Fields(v)
		}
	case []interface{}:
		scopes := make([]string, 0, len(v))
		for _, e := range v {
			if s, ok := e.(string); ok && s != "" {
				scopes = append(scopes, s)
			}
		}
		return scopes
	}
	return nil
}

// audienceContains reports whether the JWT "aud" claim — which per RFC 7519
// may be a single string or an array of strings — contains resourceURI.
func audienceContains(aud interface{}, resourceURI string) bool {
	if resourceURI == "" {
		return false
	}
	switch v := aud.(type) {
	case string:
		return v == resourceURI
	case []interface{}:
		for _, a := range v {
			if s, ok := a.(string); ok && s == resourceURI {
				return true
			}
		}
	case []string:
		for _, s := range v {
			if s == resourceURI {
				return true
			}
		}
	}
	return false
}
