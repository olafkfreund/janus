// Package redaction implements lightweight, dependency-free PII/secret
// detection and masking ("DLP") for tool call inputs and outputs flowing
// through the gateway. It never logs or returns the sensitive values it
// finds — only a redacted copy of the text and a per-class hit count.
package redaction

import (
	"fmt"
	"regexp"
	"sort"
)

// Config controls Redactor construction.
type Config struct {
	// Enabled toggles whether the Redact* methods actually mask anything.
	// When false, RedactBytes/RedactMap leave content untouched (aside from
	// the deep-copy RedactMap always performs).
	Enabled bool

	// CustomRules lets callers register additional patterns on top of the
	// built-in detectors. Each rule's matches are masked with its Class tag.
	CustomRules []Rule
}

// Rule is a single user-supplied redaction pattern.
type Rule struct {
	// Class labels the kind of data this rule detects, e.g. "internal_id".
	// It is used both to tag redacted matches and to aggregate Finding
	// counts.
	Class string

	// Pattern is a Go regexp (RE2 syntax) matched against raw text.
	Pattern string
}

// Finding summarizes how many matches of a given class were redacted. It
// never contains the matched values themselves.
type Finding struct {
	Class string
	Count int
}

// compiledRule is a fully resolved detector: a compiled pattern, an
// optional extra validator (e.g. the credit-card Luhn check), and an
// optional replacement template. When replaceTpl is empty, the whole match
// is replaced with "[REDACTED:<class>]"; otherwise replaceTpl is expanded
// against the match (regexp.Regexp.ExpandString semantics, e.g. "${1}").
type compiledRule struct {
	class      string
	re         *regexp.Regexp
	validate   func(match string) bool
	replaceTpl string
}

// Redactor holds a compiled set of built-in and custom redaction rules.
type Redactor struct {
	enabled bool
	rules   []compiledRule
}

// New compiles the built-in detectors plus any custom rules from cfg. It
// returns an error if a custom rule's pattern fails to compile as a Go
// regexp.
func New(cfg Config) (*Redactor, error) {
	rules := builtinRules()

	for _, cr := range cfg.CustomRules {
		re, err := regexp.Compile(cr.Pattern)
		if err != nil {
			return nil, fmt.Errorf("redaction: invalid custom rule %q: %w", cr.Class, err)
		}
		rules = append(rules, compiledRule{class: cr.Class, re: re})
	}

	return &Redactor{enabled: cfg.Enabled, rules: rules}, nil
}

// RedactBytes masks secrets/PII found in b and returns the redacted copy
// plus a summary of the classes matched. If the redactor is disabled, b is
// returned unchanged and findings is nil.
func (r *Redactor) RedactBytes(b []byte) ([]byte, []Finding) {
	if !r.enabled {
		return b, nil
	}

	text := string(b)
	counts := make(map[string]int)

	for _, rule := range r.rules {
		var n int
		text, n = applyRule(text, rule)
		if n > 0 {
			counts[rule.class] += n
		}
	}

	return []byte(text), findingsFromCounts(counts)
}

// RedactMap walks a decoded JSON-like map (string, map[string]any, []any,
// and other JSON leaf types), redacting string values wherever they occur.
// It returns a new map — the input m and any of its nested containers are
// never mutated — plus an aggregated summary of findings across the whole
// structure.
func (r *Redactor) RedactMap(m map[string]any) (map[string]any, []Finding) {
	counts := make(map[string]int)
	out, _ := r.redactValue(m, counts).(map[string]any)
	return out, findingsFromCounts(counts)
}

// redactValue recursively copies v, redacting any string values found and
// tallying per-class hit counts into counts. Non-string, non-container
// leaf values (numbers, bools, nil, ...) are copied through unchanged.
func (r *Redactor) redactValue(v any, counts map[string]int) any {
	switch vv := v.(type) {
	case string:
		redacted, findings := r.RedactBytes([]byte(vv))
		for _, f := range findings {
			counts[f.Class] += f.Count
		}
		return string(redacted)
	case map[string]any:
		out := make(map[string]any, len(vv))
		for k, val := range vv {
			out[k] = r.redactValue(val, counts)
		}
		return out
	case []any:
		out := make([]any, len(vv))
		for i, val := range vv {
			out[i] = r.redactValue(val, counts)
		}
		return out
	default:
		return vv
	}
}

// applyRule runs a single compiled rule over text, replacing every match
// that passes optional validation with its redaction tag (or template). It
// returns the resulting text and the number of matches redacted.
func applyRule(text string, rule compiledRule) (string, int) {
	matches := rule.re.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return text, 0
	}

	var out []byte
	last := 0
	count := 0

	for _, m := range matches {
		start, end := m[0], m[1]
		if start < last {
			// Defensive: regexp already returns non-overlapping matches,
			// but never re-redact a span another match already consumed.
			continue
		}
		if rule.validate != nil && !rule.validate(text[start:end]) {
			continue
		}

		out = append(out, text[last:start]...)
		if rule.replaceTpl != "" {
			out = rule.re.ExpandString(out, rule.replaceTpl, text, m)
		} else {
			out = append(out, "[REDACTED:"+rule.class+"]"...)
		}
		last = end
		count++
	}
	out = append(out, text[last:]...)

	if count == 0 {
		return text, 0
	}
	return string(out), count
}

// findingsFromCounts converts a class->count map into a sorted,
// deterministic Finding slice, or nil if there were no hits.
func findingsFromCounts(counts map[string]int) []Finding {
	if len(counts) == 0 {
		return nil
	}

	classes := make([]string, 0, len(counts))
	for c := range counts {
		classes = append(classes, c)
	}
	sort.Strings(classes)

	findings := make([]Finding, 0, len(classes))
	for _, c := range classes {
		findings = append(findings, Finding{Class: c, Count: counts[c]})
	}
	return findings
}
