package observer

import (
	"testing"
)

func TestInferFormBody_NilOnEmpty(t *testing.T) {
	if got := InferFormBody(nil); got != nil {
		t.Fatalf("expected nil for nil input, got %+v", got)
	}
	if got := InferFormBody([]byte{}); got != nil {
		t.Fatalf("expected nil for empty input, got %+v", got)
	}
}

func TestInferFormBody_StringType(t *testing.T) {
	got := InferFormBody([]byte(`name=Alice&city=Wonderland`))
	if got == nil {
		t.Fatal("expected non-nil schema")
	}
	if got.Type != "object" {
		t.Errorf("Type: want object, got %q", got.Type)
	}

	nameProp, ok := got.Properties["name"]
	if !ok {
		t.Fatal("expected 'name' property")
	}
	if nameProp.Type != "string" {
		t.Errorf("name type: want string, got %q", nameProp.Type)
	}
}

func TestInferFormBody_BooleanType(t *testing.T) {
	tests := []struct{ val, key string }{
		{"true", "active"},
		{"false", "deleted"},
		{"True", "upper"},
		{"FALSE", "caps"},
	}
	for _, tc := range tests {
		t.Run(tc.val, func(t *testing.T) {
			got := InferFormBody([]byte(tc.key + "=" + tc.val))
			if got == nil {
				t.Fatal("expected non-nil schema")
			}
			prop, ok := got.Properties[tc.key]
			if !ok {
				t.Fatalf("expected property %q", tc.key)
			}
			if prop.Type != "boolean" {
				t.Errorf("%s type: want boolean, got %q", tc.key, prop.Type)
			}
		})
	}
}

func TestInferFormBody_IntegerType(t *testing.T) {
	got := InferFormBody([]byte(`count=42&offset=0`))
	if got == nil {
		t.Fatal("expected non-nil schema")
	}

	countProp, ok := got.Properties["count"]
	if !ok {
		t.Fatal("expected 'count' property")
	}
	if countProp.Type != "integer" {
		t.Errorf("count type: want integer, got %q", countProp.Type)
	}
}

func TestInferFormBody_NumberType(t *testing.T) {
	got := InferFormBody([]byte(`price=19.99&ratio=0.5`))
	if got == nil {
		t.Fatal("expected non-nil schema")
	}

	priceProp, ok := got.Properties["price"]
	if !ok {
		t.Fatal("expected 'price' property")
	}
	if priceProp.Type != "number" {
		t.Errorf("price type: want number, got %q", priceProp.Type)
	}
}

func TestInferFormBody_ArrayKeys(t *testing.T) {
	got := InferFormBody([]byte(`tags[]=go&tags[]=http&tags[]=api`))
	if got == nil {
		t.Fatal("expected non-nil schema")
	}

	tagsProp, ok := got.Properties["tags"]
	if !ok {
		t.Fatal("expected 'tags' property (without [] suffix)")
	}
	if tagsProp.Type != "array" {
		t.Errorf("tags type: want array, got %q", tagsProp.Type)
	}
	if tagsProp.Items == nil {
		t.Fatal("expected Items on array schema")
	}
	if tagsProp.Items.Type != "string" {
		t.Errorf("tags items type: want string, got %q", tagsProp.Items.Type)
	}
}

func TestInferFormBody_ArrayWithIntegerValues(t *testing.T) {
	got := InferFormBody([]byte(`ids[]=1&ids[]=2&ids[]=3`))
	if got == nil {
		t.Fatal("expected non-nil schema")
	}

	idsProp, ok := got.Properties["ids"]
	if !ok {
		t.Fatal("expected 'ids' property")
	}
	if idsProp.Type != "array" {
		t.Errorf("ids type: want array, got %q", idsProp.Type)
	}
	if idsProp.Items.Type != "integer" {
		t.Errorf("ids items type: want integer, got %q", idsProp.Items.Type)
	}
}

func TestInferFormBody_MixedTypes(t *testing.T) {
	got := InferFormBody([]byte(`name=Bob&age=30&score=98.6&active=true`))
	if got == nil {
		t.Fatal("expected non-nil schema")
	}

	cases := []struct {
		key      string
		wantType string
	}{
		{"name", "string"},
		{"age", "integer"},
		{"score", "number"},
		{"active", "boolean"},
	}

	for _, tc := range cases {
		prop, ok := got.Properties[tc.key]
		if !ok {
			t.Fatalf("expected property %q", tc.key)
		}
		if prop.Type != tc.wantType {
			t.Errorf("%s: want %q, got %q", tc.key, tc.wantType, prop.Type)
		}
	}
}
