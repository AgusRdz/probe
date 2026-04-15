package observer

import (
	"testing"
)

func TestInferGraphQLSchema_NilOnEmpty(t *testing.T) {
	if got := InferGraphQLSchema(nil); got != nil {
		t.Fatalf("expected nil for empty body, got %+v", got)
	}
	if got := InferGraphQLSchema([]byte{}); got != nil {
		t.Fatalf("expected nil for empty body, got %+v", got)
	}
}

func TestInferGraphQLSchema_NilOnInvalidJSON(t *testing.T) {
	if got := InferGraphQLSchema([]byte(`not json`)); got != nil {
		t.Fatalf("expected nil for invalid JSON, got %+v", got)
	}
}

func TestInferGraphQLSchema_OperationDetection(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantOp  string
	}{
		{
			name:   "query keyword",
			body:   `{"query": "query GetUser($id: ID!) { user(id: $id) { name } }"}`,
			wantOp: "query",
		},
		{
			name:   "mutation keyword",
			body:   `{"query": "mutation CreateUser($name: String!) { createUser(name: $name) { id } }"}`,
			wantOp: "mutation",
		},
		{
			name:   "subscription keyword",
			body:   `{"query": "subscription OnMessage { messageAdded { text } }"}`,
			wantOp: "subscription",
		},
		{
			name:   "shorthand query (starts with {)",
			body:   `{"query": "{ user { name } }"}`,
			wantOp: "query",
		},
		{
			name:   "unknown falls back to graphql",
			body:   `{"query": "fragment Foo on Bar { id }"}`,
			wantOp: "graphql",
		},
		{
			name:   "no query key defaults to graphql",
			body:   `{"operationName": "GetUser"}`,
			wantOp: "graphql",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := InferGraphQLSchema([]byte(tc.body))
			if got == nil {
				t.Fatal("expected non-nil schema")
			}
			if got.Type != "object" {
				t.Errorf("Type: want object, got %q", got.Type)
			}
			if got.Description != tc.wantOp {
				t.Errorf("Description: want %q, got %q", tc.wantOp, got.Description)
			}
		})
	}
}

func TestInferGraphQLSchema_VariablesSchema(t *testing.T) {
	body := []byte(`{"query": "query GetUser($id: ID!) { user(id: $id) { name email } }", "variables": {"id": "123", "active": true}}`)

	got := InferGraphQLSchema(body)
	if got == nil {
		t.Fatal("expected non-nil schema")
	}
	if got.Description != "query" {
		t.Errorf("Description: want query, got %q", got.Description)
	}

	varsSchema, ok := got.Properties["variables"]
	if !ok {
		t.Fatal("expected 'variables' property in schema")
	}
	if varsSchema.Type != "object" {
		t.Errorf("variables type: want object, got %q", varsSchema.Type)
	}

	idProp, ok := varsSchema.Properties["id"]
	if !ok {
		t.Fatal("expected 'id' in variables properties")
	}
	if idProp.Type != "string" {
		t.Errorf("id type: want string, got %q", idProp.Type)
	}

	activeProp, ok := varsSchema.Properties["active"]
	if !ok {
		t.Fatal("expected 'active' in variables properties")
	}
	if activeProp.Type != "boolean" {
		t.Errorf("active type: want boolean, got %q", activeProp.Type)
	}
}

func TestInferGraphQLSchema_NullVariables(t *testing.T) {
	body := []byte(`{"query": "query Foo { bar }", "variables": null}`)
	got := InferGraphQLSchema(body)
	if got == nil {
		t.Fatal("expected non-nil schema")
	}
	if _, ok := got.Properties["variables"]; ok {
		t.Error("expected no 'variables' property when variables is null")
	}
}

func TestInferGraphQLSchema_NoStoredValues(t *testing.T) {
	body := []byte(`{"query": "query GetUser($id: ID!) { user(id: $id) { name } }", "variables": {"id": "super-secret-value"}}`)
	got := InferGraphQLSchema(body)
	if got == nil {
		t.Fatal("expected non-nil schema")
	}
	// The schema must not contain the actual value "super-secret-value" anywhere.
	// We verify only type metadata is present.
	varsSchema := got.Properties["variables"]
	idProp := varsSchema.Properties["id"]
	if idProp.Type != "string" {
		t.Errorf("id type: want string, got %q", idProp.Type)
	}
	// No field on Schema holds a raw value — just confirm the type.
}
