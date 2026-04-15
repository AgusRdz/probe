package observer

import (
	"encoding/json"
	"testing"
)

// TestSchemaJSONRoundTrip verifies that a Schema with nested Properties
// marshals and unmarshals correctly, preserving all fields.
func TestSchemaJSONRoundTrip(t *testing.T) {
	minLen := 1
	maxLen := 64
	min := 0.0
	max := 100.0

	original := &Schema{
		Type:   "object",
		Format: "uuid",
		Properties: map[string]*Schema{
			"id": {
				Type:      "string",
				Format:    "uuid",
				MinLength: minLen,
				MaxLength: maxLen,
			},
			"score": {
				Type:    "number",
				Minimum: &min,
				Maximum: &max,
			},
			"tags": {
				Type: "array",
				Items: &Schema{
					Type: "string",
				},
			},
		},
		Required:    []string{"id"},
		Nullable:    true,
		Enum:        []string{"active", "inactive"},
		Pattern:     "^[a-z]+$",
		Description: "top-level object",
		XMLAttr:     true,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Schema
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Type != original.Type {
		t.Errorf("Type: got %q, want %q", decoded.Type, original.Type)
	}
	if decoded.Format != original.Format {
		t.Errorf("Format: got %q, want %q", decoded.Format, original.Format)
	}
	if len(decoded.Properties) != len(original.Properties) {
		t.Errorf("Properties len: got %d, want %d", len(decoded.Properties), len(original.Properties))
	}
	idProp, ok := decoded.Properties["id"]
	if !ok {
		t.Fatal("Properties[\"id\"] missing after round-trip")
	}
	if idProp.Type != "string" {
		t.Errorf("Properties[id].Type: got %q, want %q", idProp.Type, "string")
	}
	if idProp.MinLength != minLen {
		t.Errorf("Properties[id].MinLength: got %d, want %d", idProp.MinLength, minLen)
	}
	if idProp.MaxLength != maxLen {
		t.Errorf("Properties[id].MaxLength: got %d, want %d", idProp.MaxLength, maxLen)
	}

	scoreProp, ok := decoded.Properties["score"]
	if !ok {
		t.Fatal("Properties[\"score\"] missing after round-trip")
	}
	if scoreProp.Minimum == nil || *scoreProp.Minimum != min {
		t.Errorf("Properties[score].Minimum: got %v, want %v", scoreProp.Minimum, min)
	}
	if scoreProp.Maximum == nil || *scoreProp.Maximum != max {
		t.Errorf("Properties[score].Maximum: got %v, want %v", scoreProp.Maximum, max)
	}

	tagsProp, ok := decoded.Properties["tags"]
	if !ok {
		t.Fatal("Properties[\"tags\"] missing after round-trip")
	}
	if tagsProp.Items == nil || tagsProp.Items.Type != "string" {
		t.Errorf("Properties[tags].Items: got %v, want &Schema{Type:\"string\"}", tagsProp.Items)
	}

	if len(decoded.Required) != 1 || decoded.Required[0] != "id" {
		t.Errorf("Required: got %v, want [\"id\"]", decoded.Required)
	}
	if !decoded.Nullable {
		t.Error("Nullable: got false, want true")
	}
	if len(decoded.Enum) != 2 {
		t.Errorf("Enum: got %v, want [active inactive]", decoded.Enum)
	}
	if decoded.Pattern != original.Pattern {
		t.Errorf("Pattern: got %q, want %q", decoded.Pattern, original.Pattern)
	}
	if decoded.Description != original.Description {
		t.Errorf("Description: got %q, want %q", decoded.Description, original.Description)
	}
	if !decoded.XMLAttr {
		t.Error("XMLAttr: got false, want true")
	}
}

// TestSchemaNilItemsOmitted verifies that a nil Items field is omitted from
// marshaled JSON (omitempty), not serialized as "items":null.
func TestSchemaNilItemsOmitted(t *testing.T) {
	s := &Schema{
		Type: "string",
	}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}

	if _, present := raw["items"]; present {
		t.Error("\"items\" key present in JSON when Items is nil; want omitted")
	}
	if _, present := raw["properties"]; present {
		t.Error("\"properties\" key present in JSON when Properties is nil; want omitted")
	}
	if _, present := raw["required"]; present {
		t.Error("\"required\" key present in JSON when Required is nil; want omitted")
	}
}

// TestCapturedPairZeroValue verifies that the zero value of CapturedPair is
// valid (no panics, no required fields, sane defaults).
func TestCapturedPairZeroValue(t *testing.T) {
	var p CapturedPair

	if p.Method != "" {
		t.Errorf("Method zero: got %q, want empty string", p.Method)
	}
	if p.RawPath != "" {
		t.Errorf("RawPath zero: got %q, want empty string", p.RawPath)
	}
	if p.StatusCode != 0 {
		t.Errorf("StatusCode zero: got %d, want 0", p.StatusCode)
	}
	if p.LatencyMs != 0 {
		t.Errorf("LatencyMs zero: got %d, want 0", p.LatencyMs)
	}
	if p.ReqBody != nil {
		t.Errorf("ReqBody zero: got %v, want nil", p.ReqBody)
	}
	if p.RespBody != nil {
		t.Errorf("RespBody zero: got %v, want nil", p.RespBody)
	}
}
