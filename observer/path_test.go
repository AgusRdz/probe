package observer

import "testing"

func TestNormalizeSegment(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		// Rule 1: pure integers
		{"42", "{id}"},
		{"0", "{id}"},
		{"123456789", "{id}"},

		// Rule 2: UUID
		{"550e8400-e29b-41d4-a716-446655440000", "{id}"},

		// Rule 3: ULID (26 chars, Crockford base32)
		{"01ARZ3NDEKTSV4RRFFQ69G5FAV", "{id}"},

		// Rule 5: slug with numeric suffix
		{"abc-42", "{id}"},

		// Rule 6: ALL-CAPS alphanumeric with optional hyphens
		{"ORD-9821", "{id}"},
		{"ABC123", "{id}"},

		// Rule 8: known keywords — keep as-is
		{"me", "me"},
		{"self", "self"},
		{"current", "current"},
		{"search", "search"},

		// Rule 10: resource names — keep as-is
		{"users", "users"},
		{"orders", "orders"},

		// Rule 6 edge: too short (len < 3) — keep as-is
		{"a", "a"},

		// Rule 10 edge: short version segment — keep as-is
		{"v1", "v1"},

		// Rule 9: long string > 32 chars
		{"aVeryLongStringThatIsDefinitelyMoreThan32Characters", "{id}"},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := NormalizeSegment(tc.input)
			if got != tc.want {
				t.Errorf("NormalizeSegment(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestNormalizePath(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"/", "/"},
		{"/users", "/users"},
		{"/users/42", "/users/{id}"},
		{"/users/42/orders", "/users/{id}/orders"},
		{"/users/me", "/users/me"},
		{"/users/me/settings", "/users/me/settings"},
		{"/api/v1/users/42", "/api/v1/users/{id}"},
		{"/orders/ORD-9821/items", "/orders/{id}/items"},
		{"", "/"},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := NormalizePath(tc.input)
			if got != tc.want {
				t.Errorf("NormalizePath(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestIsLikelyID(t *testing.T) {
	cases := []struct {
		seg  string
		want bool
	}{
		{"42", true},
		{"550e8400-e29b-41d4-a716-446655440000", true},
		{"01ARZ3NDEKTSV4RRFFQ69G5FAV", true},
		{"abc-42", true},
		{"ORD-9821", true},
		{"users", false},
		{"me", false},
		{"v1", false},
	}

	for _, tc := range cases {
		t.Run(tc.seg, func(t *testing.T) {
			got := IsLikelyID(tc.seg)
			if got != tc.want {
				t.Errorf("IsLikelyID(%q) = %v, want %v", tc.seg, got, tc.want)
			}
		})
	}
}
