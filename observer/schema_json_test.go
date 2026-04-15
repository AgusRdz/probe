package observer

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestDetectStringFormat(t *testing.T) {
	cases := []struct {
		input  string
		format string
	}{
		{"hello", ""},
		{"a@b.com", "email"},
		{"user.name+tag@example.co.uk", "email"},
		{"550e8400-e29b-41d4-a716-446655440000", "uuid"},
		{"550E8400-E29B-41D4-A716-446655440000", "uuid"},
		{"2024-01-15T10:00:00Z", "date-time"},
		{"2024-01-15T10:00:00+05:30", "date-time"},
		{"2024-01-15T10:00:00.123Z", "date-time"},
		{"2024-01-15", "date"},
		{"https://example.com", "uri"},
		{"http://example.com/path?q=1", "uri"},
		{"192.168.1.1", "ipv4"},
		{"255.255.255.0", "ipv4"},
		{"2001:0db8:85a3:0000:0000:8a2e:0370:7334", "ipv6"},
		{"::1", "ipv6"},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := detectStringFormat(tc.input)
			if got != tc.format {
				t.Errorf("detectStringFormat(%q) = %q, want %q", tc.input, got, tc.format)
			}
		})
	}
}

func TestInferJSONSchema(t *testing.T) {
	cases := []struct {
		name  string
		input any
		want  Schema
	}{
		{
			name:  "nil",
			input: nil,
			want:  Schema{Nullable: true},
		},
		{
			name:  "bool true",
			input: true,
			want:  Schema{Type: "boolean"},
		},
		{
			name:  "bool false",
			input: false,
			want:  Schema{Type: "boolean"},
		},
		{
			name:  "float64 integer",
			input: float64(42),
			want:  Schema{Type: "integer"},
		},
		{
			name:  "float64 number",
			input: float64(3.14),
			want:  Schema{Type: "number"},
		},
		{
			name:  "string plain",
			input: "hello",
			want:  Schema{Type: "string", Format: ""},
		},
		{
			name:  "string email",
			input: "a@b.com",
			want:  Schema{Type: "string", Format: "email"},
		},
		{
			name:  "string uuid",
			input: "550e8400-e29b-41d4-a716-446655440000",
			want:  Schema{Type: "string", Format: "uuid"},
		},
		{
			name:  "string date-time",
			input: "2024-01-15T10:00:00Z",
			want:  Schema{Type: "string", Format: "date-time"},
		},
		{
			name:  "string date",
			input: "2024-01-15",
			want:  Schema{Type: "string", Format: "date"},
		},
		{
			name:  "string uri",
			input: "https://example.com",
			want:  Schema{Type: "string", Format: "uri"},
		},
		{
			name:  "empty array",
			input: []any{},
			want:  Schema{Type: "array", Items: &Schema{Type: "object"}},
		},
		{
			name:  "array with one string",
			input: []any{"hello"},
			want:  Schema{Type: "array", Items: &Schema{Type: "string"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := InferJSONSchema(tc.input)
			if !schemasEqual(got, tc.want) {
				t.Errorf("InferJSONSchema(%v)\ngot:  %+v\nwant: %+v", tc.input, got, tc.want)
			}
		})
	}
}

func TestInferJSONSchema_NestedObject(t *testing.T) {
	input := map[string]any{
		"name": "Alice",
		"address": map[string]any{
			"city": "NYC",
			"zip":  float64(10001),
		},
	}

	got := InferJSONSchema(input)

	if got.Type != "object" {
		t.Fatalf("expected type=object, got %q", got.Type)
	}
	nameProp, ok := got.Properties["name"]
	if !ok || nameProp.Type != "string" {
		t.Errorf("expected properties.name to be string schema")
	}
	addrProp, ok := got.Properties["address"]
	if !ok || addrProp.Type != "object" {
		t.Errorf("expected properties.address to be object schema")
	}
	if addrProp.Properties["city"] == nil || addrProp.Properties["city"].Type != "string" {
		t.Errorf("expected address.city to be string schema")
	}
	if addrProp.Properties["zip"] == nil || addrProp.Properties["zip"].Type != "integer" {
		t.Errorf("expected address.zip to be integer schema")
	}
}

func TestInferJSONSchema_ArrayMergedSchema(t *testing.T) {
	// Some items have "email", ALL items have "name".
	// Merged schema: "name" is required, "email" is not.
	input := []any{
		map[string]any{"name": "Alice", "email": "alice@example.com"},
		map[string]any{"name": "Bob"},
		map[string]any{"name": "Carol", "email": "carol@example.com"},
	}

	got := InferJSONSchema(input)

	if got.Type != "array" {
		t.Fatalf("expected type=array, got %q", got.Type)
	}
	if got.Items == nil {
		t.Fatal("expected Items to be non-nil")
	}
	items := got.Items
	if items.Type != "object" {
		t.Fatalf("expected items.type=object, got %q", items.Type)
	}

	// "name" must be in Required.
	hasName := false
	for _, r := range items.Required {
		if r == "name" {
			hasName = true
		}
	}
	if !hasName {
		t.Errorf("expected 'name' to be required, got Required=%v", items.Required)
	}

	// "email" must NOT be in Required.
	for _, r := range items.Required {
		if r == "email" {
			t.Errorf("expected 'email' to NOT be required, but found it in Required=%v", items.Required)
		}
	}

	// Both fields must exist in Properties.
	if items.Properties["name"] == nil {
		t.Error("expected 'name' in Properties")
	}
	if items.Properties["email"] == nil {
		t.Error("expected 'email' in Properties")
	}
}

func TestInferJSONBody(t *testing.T) {
	t.Run("nil bytes", func(t *testing.T) {
		if got := InferJSONBody(nil); got != nil {
			t.Errorf("expected nil, got %+v", got)
		}
	})

	t.Run("empty bytes", func(t *testing.T) {
		if got := InferJSONBody([]byte{}); got != nil {
			t.Errorf("expected nil, got %+v", got)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		if got := InferJSONBody([]byte(`{not valid}`)); got != nil {
			t.Errorf("expected nil, got %+v", got)
		}
	})

	t.Run("valid JSON object", func(t *testing.T) {
		data, _ := json.Marshal(map[string]any{"id": float64(1), "name": "probe"})
		got := InferJSONBody(data)
		if got == nil {
			t.Fatal("expected non-nil Schema")
		}
		if got.Type != "object" {
			t.Errorf("expected type=object, got %q", got.Type)
		}
	})

	t.Run("valid JSON array", func(t *testing.T) {
		got := InferJSONBody([]byte(`[1, 2, 3]`))
		if got == nil {
			t.Fatal("expected non-nil Schema")
		}
		if got.Type != "array" {
			t.Errorf("expected type=array, got %q", got.Type)
		}
	})
}

// schemasEqual compares two Schema values for test assertions.
// Uses reflect.DeepEqual but treats nil and empty slice as equal for Required/Enum.
func schemasEqual(a, b Schema) bool {
	// Normalise nil vs empty slices so tests don't fail on that distinction.
	if len(a.Required) == 0 && len(b.Required) == 0 {
		a.Required = nil
		b.Required = nil
	}
	if len(a.Enum) == 0 && len(b.Enum) == 0 {
		a.Enum = nil
		b.Enum = nil
	}
	return reflect.DeepEqual(a, b)
}
