package redaction

import (
	"regexp"
	"strings"
)

// Built-in detector class labels. These are attached to Finding.Class and
// used as the "[REDACTED:<class>]" mask tag.
const (
	ClassEmail      = "email"
	ClassCreditCard = "credit_card"
	ClassJWT        = "jwt"
	ClassAWSKey     = "aws_access_key"
	ClassSecret     = "secret"
	ClassIBAN       = "iban"
)

var (
	// Email addresses.
	emailRe = regexp.MustCompile(`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`)

	// JWTs are three dot-separated base64url segments. Requiring the header
	// segment to start with "eyJ" (the base64 encoding of `{"`) keeps this
	// from matching arbitrary dotted strings or version numbers.
	jwtRe = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\b`)

	// AWS access key IDs, e.g. AKIAIOSFODNN7EXAMPLE.
	awsKeyRe = regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)

	// Candidate credit-card-shaped digit runs, optionally grouped with
	// spaces or dashes (e.g. "4111 1111 1111 1111"). A match here is only a
	// candidate: isValidCreditCard applies the Luhn checksum before it is
	// accepted, so arbitrary long digit sequences (phone numbers, order
	// IDs, ...) are not flagged.
	creditCardRe = regexp.MustCompile(`\b\d(?:[ -]?\d){12,18}\b`)

	// IBAN: 2-letter country code, 2 check digits, 11-30 alphanumerics.
	ibanRe = regexp.MustCompile(`\b[A-Z]{2}[0-9]{2}[A-Z0-9]{11,30}\b`)

	// Generic secret keyed off assignment context, e.g. `api_key=...` or
	// `token: ...`. Requires a long (>=32 char) opaque value so ordinary,
	// short, human-readable config values are left alone.
	secretAssignRe = regexp.MustCompile(`(?i)\b(api[_-]?key|access[_-]?key|token|secret|password|passwd)\s*[:=]\s*"?([A-Za-z0-9_\-/+=]{32,})"?`)

	// "Authorization: Bearer <token>" style headers.
	bearerRe = regexp.MustCompile(`(?i)(\bBearer\s+)([A-Za-z0-9\-_.=]{20,})`)

	// OpenAI-style "sk-..." API keys.
	openAIKeyRe = regexp.MustCompile(`\bsk-[A-Za-z0-9]{20,}\b`)
)

// luhnValid reports whether digits (a string of ASCII '0'-'9' characters)
// passes the Luhn checksum used by all major card networks.
func luhnValid(digits string) bool {
	sum := 0
	double := false
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i] - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return sum%10 == 0
}

// isValidCreditCard validates a raw regex match as a plausible real card
// number: 13-19 digits once separators are stripped, all-numeric, and
// Luhn-valid. This is what keeps random 16-digit numbers from being
// misidentified as credit cards.
func isValidCreditCard(match string) bool {
	digits := strings.NewReplacer(" ", "", "-", "").Replace(match)
	if len(digits) < 13 || len(digits) > 19 {
		return false
	}
	for _, c := range digits {
		if c < '0' || c > '9' {
			return false
		}
	}
	return luhnValid(digits)
}

// builtinRules returns the built-in detector set, evaluated in order. More
// specific detectors (JWT, AWS keys) run before generic, context-based ones
// (assignment-keyed secrets, bearer tokens) so a value ends up tagged with
// its most informative class and isn't redacted more than once.
func builtinRules() []compiledRule {
	return []compiledRule{
		{class: ClassEmail, re: emailRe},
		{class: ClassJWT, re: jwtRe},
		{class: ClassAWSKey, re: awsKeyRe},
		{class: ClassCreditCard, re: creditCardRe, validate: isValidCreditCard},
		{class: ClassIBAN, re: ibanRe},
		{class: ClassSecret, re: secretAssignRe, replaceTpl: `${1}=[REDACTED:secret]`},
		{class: ClassSecret, re: bearerRe, replaceTpl: `${1}[REDACTED:secret]`},
		{class: ClassSecret, re: openAIKeyRe},
	}
}
