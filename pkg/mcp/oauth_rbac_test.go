package mcp

import (
	"reflect"
	"sort"
	"testing"

	"github.com/calitti/mcp-api-gateway/pkg/oauth"
)

func newRBAC() *oauthRBAC {
	s := &MCPServer{}
	s.SetOAuthRBAC("groups",
		map[string][]string{
			"finance": {"lch_*", "fx_*"},
			"crypto":  {"coinbase_*", "fx_*"},
			"ops":     {"admin_*"},
		},
		[]string{"ops"},
	)
	return s.oauthRBAC
}

func TestOAuthRBAC_Resolve(t *testing.T) {
	m := newRBAC()

	tests := []struct {
		name       string
		groups     any // value of the "groups" claim
		wantRole   string
		wantScopes []string
	}{
		{"single mapped group (string claim)", "finance", "user", []string{"fx_*", "lch_*"}},
		{"multiple groups union+dedup", []any{"finance", "crypto"}, "user", []string{"coinbase_*", "fx_*", "lch_*"}},
		{"admin group grants admin role", []any{"ops"}, "admin", []string{"admin_*"}},
		{"unmapped group is fail-closed", []any{"interns"}, "user", nil},
		{"no groups claim is fail-closed", nil, "user", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := map[string]any{"sub": "u1"}
			if tt.groups != nil {
				raw["groups"] = tt.groups
			}
			role, scopes := m.resolve(&oauth.Claims{Subject: "u1", Raw: raw})
			if role != tt.wantRole {
				t.Errorf("role = %q, want %q", role, tt.wantRole)
			}
			sort.Strings(scopes)
			if !reflect.DeepEqual(scopes, tt.wantScopes) {
				t.Errorf("scopes = %v, want %v", scopes, tt.wantScopes)
			}
		})
	}
}

// SetOAuthRBAC with no mapping must leave the feature disabled so OAuth
// clients keep their default flat "user" + token-scopes behavior.
func TestSetOAuthRBAC_DisabledWhenEmpty(t *testing.T) {
	s := &MCPServer{}
	s.SetOAuthRBAC("groups", nil, nil)
	if s.oauthRBAC != nil {
		t.Fatal("empty mapping should leave oauthRBAC nil (disabled)")
	}
}
