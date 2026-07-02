package redaction

import (
	"strings"
	"testing"
)

func newEnabledRedactor(t *testing.T, cfg Config) *Redactor {
	t.Helper()
	cfg.Enabled = true
	r, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return r
}

// findingCount returns the Count for class, or 0 if class has no findings.
func findingCount(findings []Finding, class string) int {
	for _, f := range findings {
		if f.Class == class {
			return f.Count
		}
	}
	return 0
}

func TestRedactBytes_TruePositives(t *testing.T) {
	r := newEnabledRedactor(t, Config{})

	tests := []struct {
		name      string
		input     string
		class     string
		wantCount int
	}{
		{
			name:      "email address",
			input:     "Please contact jane.doe@example.com for details.",
			class:     ClassEmail,
			wantCount: 1,
		},
		{
			name:      "credit card with spaces (Luhn valid)",
			input:     "Card on file: 4111 1111 1111 1111, exp 12/29.",
			class:     ClassCreditCard,
			wantCount: 1,
		},
		{
			name:      "credit card with dashes (Luhn valid)",
			input:     "Card on file: 4111-1111-1111-1111, exp 12/29.",
			class:     ClassCreditCard,
			wantCount: 1,
		},
		{
			name: "jwt",
			input: "Authorization: " +
				"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9." +
				"eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ." +
				"SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
			class:     ClassJWT,
			wantCount: 1,
		},
		{
			name:      "aws access key id",
			input:     "aws_access_key_id = AKIAIOSFODNN7EXAMPLE",
			class:     ClassAWSKey,
			wantCount: 1,
		},
		{
			name:      "iban",
			input:     "Please wire funds to IBAN: GB29NWBK60161331926819",
			class:     ClassIBAN,
			wantCount: 1,
		},
		{
			name:      "api key assignment (hex, 64 chars)",
			input:     "config: api_key=9f8e7d6c5b4a3f2e1d0c9b8a7f6e5d4c3b2a1f0e9d8c7b6a5f4e3d2c1b0a9f8e",
			class:     ClassSecret,
			wantCount: 1,
		},
		{
			name:      "token assignment with colon",
			input:     `token: "9f8e7d6c5b4a3f2e1d0c9b8a7f6e5d4c3b2a1f0e9d8c7b6a5f4e3d2c1b0a9f8e"`,
			class:     ClassSecret,
			wantCount: 1,
		},
		{
			name:      "bearer token header",
			input:     "Authorization: Bearer abcd1234efgh5678ijkl9012mnop3456",
			class:     ClassSecret,
			wantCount: 1,
		},
		{
			name:      "openai-style sk- key",
			input:     "OPENAI_API_KEY=sk-abcdefghijklmnopqrstuvwxyz0123456789",
			class:     ClassSecret,
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, findings := r.RedactBytes([]byte(tt.input))

			if strings.Contains(string(got), tt.input) {
				t.Fatalf("RedactBytes() did not redact anything; got %q", got)
			}
			wantTag := "[REDACTED:" + tt.class + "]"
			if !strings.Contains(string(got), wantTag) {
				t.Errorf("RedactBytes() = %q, want it to contain %q", got, wantTag)
			}
			if c := findingCount(findings, tt.class); c != tt.wantCount {
				t.Errorf("findingCount(%q) = %d, want %d (findings=%v)", tt.class, c, tt.wantCount, findings)
			}
		})
	}
}

func TestRedactBytes_FalsePositives(t *testing.T) {
	r := newEnabledRedactor(t, Config{})

	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "plain sentence",
			input: "The meeting is at 3pm tomorrow, see you then.",
		},
		{
			name:  "short phone-like number",
			input: "My phone number is 555-1234, call anytime.",
		},
		{
			name:  "16-digit number failing Luhn",
			input: "Order id 1234567890123456 was shipped yesterday.",
		},
		{
			name:  "short config value below secret threshold",
			input: "user_id=42 and retries=3",
		},
		{
			name:  "text without an @ is not an email",
			input: "Contact the frontdesk, not a real email here.",
		},
		{
			name:  "short alphanumeric code is not an IBAN",
			input: "Promo code AB12 is valid until Friday.",
		},
		{
			name:  "dotted version string is not a jwt",
			input: "Upgraded to version 1.2.3 of the client library.",
		},
		{
			name:  "small numbers in prose",
			input: "I have 3 apples and 42 oranges in the basket.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, findings := r.RedactBytes([]byte(tt.input))
			if string(got) != tt.input {
				t.Errorf("RedactBytes() = %q, want unchanged %q", got, tt.input)
			}
			if findings != nil {
				t.Errorf("findings = %v, want nil", findings)
			}
		})
	}
}

func TestRedactBytes_Disabled(t *testing.T) {
	r, err := New(Config{Enabled: false})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	input := "email me at jane.doe@example.com"
	got, findings := r.RedactBytes([]byte(input))

	if string(got) != input {
		t.Errorf("RedactBytes() with disabled redactor = %q, want unchanged %q", got, input)
	}
	if findings != nil {
		t.Errorf("findings = %v, want nil", findings)
	}
}

func TestLuhnRejectsInvalidCard(t *testing.T) {
	tests := []struct {
		name  string
		match string
		want  bool
	}{
		{name: "valid visa test number", match: "4111 1111 1111 1111", want: true},
		{name: "valid visa test number with dashes", match: "4111-1111-1111-1111", want: true},
		{name: "invalid sequential digits", match: "1234567890123456", want: false},
		{name: "too short to be a card", match: "4111 1111", want: false},
		{name: "contains a letter", match: "4111-1111-1111-111X", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isValidCreditCard(tt.match); got != tt.want {
				t.Errorf("isValidCreditCard(%q) = %v, want %v", tt.match, got, tt.want)
			}
		})
	}
}

func TestRedactMap_NestedRecursionAndImmutability(t *testing.T) {
	r := newEnabledRedactor(t, Config{})

	original := map[string]any{
		"user": map[string]any{
			"email": "alice@example.com",
			"name":  "Alice",
		},
		"tags": []any{"vip", "bob@example.com"},
		"meta": map[string]any{
			"count":  float64(3),
			"active": true,
		},
	}

	redacted, findings := r.RedactMap(original)

	// Nested map value was redacted.
	user, ok := redacted["user"].(map[string]any)
	if !ok {
		t.Fatalf("redacted[\"user\"] is not a map: %#v", redacted["user"])
	}
	if got := user["email"]; got != "[REDACTED:email]" {
		t.Errorf("redacted user email = %q, want [REDACTED:email]", got)
	}
	if got := user["name"]; got != "Alice" {
		t.Errorf("redacted user name = %q, want unchanged Alice", got)
	}

	// Nested slice element was redacted.
	tags, ok := redacted["tags"].([]any)
	if !ok || len(tags) != 2 {
		t.Fatalf("redacted[\"tags\"] = %#v, want a 2-element slice", redacted["tags"])
	}
	if got := tags[1]; got != "[REDACTED:email]" {
		t.Errorf("redacted tags[1] = %q, want [REDACTED:email]", got)
	}
	if got := tags[0]; got != "vip" {
		t.Errorf("redacted tags[0] = %q, want unchanged vip", got)
	}

	// Non-string leaves pass through unchanged.
	meta, ok := redacted["meta"].(map[string]any)
	if !ok {
		t.Fatalf("redacted[\"meta\"] is not a map: %#v", redacted["meta"])
	}
	if meta["count"] != float64(3) || meta["active"] != true {
		t.Errorf("redacted meta = %#v, want count/active unchanged", meta)
	}

	// Findings aggregate across the whole structure.
	if c := findingCount(findings, ClassEmail); c != 2 {
		t.Errorf("findingCount(email) = %d, want 2 (findings=%v)", c, findings)
	}

	// The original map (and its nested containers) must not be mutated.
	origUser := original["user"].(map[string]any)
	if origUser["email"] != "alice@example.com" {
		t.Errorf("original user email mutated: %q", origUser["email"])
	}
	origTags := original["tags"].([]any)
	if origTags[1] != "bob@example.com" {
		t.Errorf("original tags[1] mutated: %q", origTags[1])
	}
}

func TestRedactMap_Disabled(t *testing.T) {
	r, err := New(Config{Enabled: false})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	original := map[string]any{"email": "alice@example.com"}
	redacted, findings := r.RedactMap(original)

	if redacted["email"] != "alice@example.com" {
		t.Errorf("redacted email = %q, want unchanged when disabled", redacted["email"])
	}
	if findings != nil {
		t.Errorf("findings = %v, want nil when disabled", findings)
	}
}

func TestNew_CustomRuleInvalidRegexp(t *testing.T) {
	_, err := New(Config{
		Enabled: true,
		CustomRules: []Rule{
			{Class: "broken", Pattern: "(unclosed["},
		},
	})
	if err == nil {
		t.Fatal("New() error = nil, want an error for an invalid custom regexp")
	}
}

func TestNew_CustomRuleRedacts(t *testing.T) {
	r := newEnabledRedactor(t, Config{
		CustomRules: []Rule{
			{Class: "internal_id", Pattern: `\bEMP-\d{6}\b`},
		},
	})

	input := "Employee record EMP-123456 was updated."
	got, findings := r.RedactBytes([]byte(input))

	if !strings.Contains(string(got), "[REDACTED:internal_id]") {
		t.Errorf("RedactBytes() = %q, want it to contain [REDACTED:internal_id]", got)
	}
	if c := findingCount(findings, "internal_id"); c != 1 {
		t.Errorf("findingCount(internal_id) = %d, want 1 (findings=%v)", c, findings)
	}
}

func BenchmarkRedactBytes(b *testing.B) {
	r, err := New(Config{Enabled: true})
	if err != nil {
		b.Fatalf("New() error = %v", err)
	}

	input := []byte(`{"email":"alice@example.com","card":"4111 1111 1111 1111","note":"nothing sensitive here"}`)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r.RedactBytes(input)
	}
}
