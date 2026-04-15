package observer

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	rePathUUID        = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	reCrockford       = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)
	reSlugNumeric     = regexp.MustCompile(`^[a-z][a-z0-9-]*-\d+$`)
	reAllCapsAlphaNum = regexp.MustCompile(`^[A-Z][A-Z0-9-]+$`)
)

// knownKeywords are kept as-is (rule 8).
var knownKeywords = map[string]bool{
	"me":      true,
	"self":    true,
	"current": true,
	"latest":  true,
	"new":     true,
	"first":   true,
	"last":    true,
	"all":     true,
	"count":   true,
	"search":  true,
}

// IsLikelyID returns true if a segment looks like an identifier (would be normalized to {id}).
func IsLikelyID(seg string) bool {
	return NormalizeSegment(seg) == "{id}"
}

// NormalizeSegment returns the normalized form of a single URL path segment.
// Returns "{id}" if the segment looks like an identifier, otherwise returns the segment as-is.
// Rules applied in order, first match wins.
func NormalizeSegment(seg string) string {
	if seg == "" {
		return seg
	}

	// Rule 1: Pure integers (all digits, len > 0).
	if isAllDigits(seg) {
		return "{id}"
	}

	// Rule 2: UUID (8-4-4-4-12 hex groups).
	if rePathUUID.MatchString(seg) {
		return "{id}"
	}

	// Rule 3: ULID — exactly 26 chars, all Crockford base32 chars.
	if len(seg) == 26 && reCrockford.MatchString(seg) {
		return "{id}"
	}

	// Rule 4: CUID2/NanoID — 21+ alphanumeric chars that are NOT all-alpha (contains digits).
	if len(seg) >= 21 && isAlphanumeric(seg) && !isAllAlpha(seg) {
		return "{id}"
	}

	// Rule 5: Slug with numeric suffix: ^[a-z][a-z0-9-]*-\d+$
	if reSlugNumeric.MatchString(seg) {
		return "{id}"
	}

	// Rule 6: ALL-CAPS alphanumeric with optional hyphens, len >= 3.
	if len(seg) >= 3 && reAllCapsAlphaNum.MatchString(seg) {
		return "{id}"
	}

	// Rule 7: Cross-call confirmation — handled at read time by store queries. Skip.

	// Rule 8: Known semantic keywords — keep as-is.
	if knownKeywords[seg] {
		return seg
	}

	// Rule 9: Long strings (len > 32).
	if len(seg) > 32 {
		return "{id}"
	}

	// Rule 10: All other segments — keep as-is.
	return seg
}

// NormalizePath converts a raw observed path to a pattern.
// Splits on "/" and normalizes each segment.
// e.g. "/users/42/orders" → "/users/{id}/orders"
// e.g. "/users/me" → "/users/me"
func NormalizePath(rawPath string) string {
	if rawPath == "" {
		return "/"
	}

	parts := strings.Split(rawPath, "/")
	for i, p := range parts {
		parts[i] = NormalizeSegment(p)
	}
	return strings.Join(parts, "/")
}

// isAllDigits returns true if s is non-empty and consists solely of ASCII digits.
func isAllDigits(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// isAlphanumeric returns true if every rune in s is a letter or digit.
func isAlphanumeric(s string) bool {
	for _, c := range s {
		if !unicode.IsLetter(c) && !unicode.IsDigit(c) {
			return false
		}
	}
	return true
}

// isAllAlpha returns true if every rune in s is a letter (no digits).
func isAllAlpha(s string) bool {
	for _, c := range s {
		if !unicode.IsLetter(c) {
			return false
		}
	}
	return true
}
