package export

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestLoadExistingYAMLMap verifies a round-trip through LoadExistingYAMLMap.
func TestLoadExistingYAMLMap(t *testing.T) {
	t.Parallel()
	src := map[string]interface{}{
		"openapi": "3.0.3",
		"info":    map[string]interface{}{"title": "Test"},
		"paths":   map[string]interface{}{},
	}
	b, err := yaml.Marshal(src)
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}
	f := filepath.Join(t.TempDir(), "spec.yaml")
	if err := os.WriteFile(f, b, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := LoadExistingYAMLMap(f)
	if err != nil {
		t.Fatalf("LoadExistingYAMLMap: %v", err)
	}
	if got["openapi"] != "3.0.3" {
		t.Errorf("openapi field: got %v", got["openapi"])
	}
}

// TestLoadExistingJSONMap verifies a round-trip through LoadExistingJSONMap.
func TestLoadExistingJSONMap(t *testing.T) {
	t.Parallel()
	src := map[string]interface{}{
		"swagger": "2.0",
		"paths":   map[string]interface{}{},
	}
	b, err := json.MarshalIndent(src, "", "  ")
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	f := filepath.Join(t.TempDir(), "spec.json")
	if err := os.WriteFile(f, b, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := LoadExistingJSONMap(f)
	if err != nil {
		t.Fatalf("LoadExistingJSONMap: %v", err)
	}
	if got["swagger"] != "2.0" {
		t.Errorf("swagger field: got %v", got["swagger"])
	}
}

// TestMergeOpenAPIPaths_AllNew verifies all incoming paths are added when existing is empty.
func TestMergeOpenAPIPaths_AllNew(t *testing.T) {
	t.Parallel()
	existing := map[string]interface{}{
		"openapi": "3.0.3",
		"paths":   map[string]interface{}{},
	}
	incoming := &OpenAPISpec{
		Paths: map[string]OpenAPIPathItem{
			"/api/users": {
				Get: &OpenAPIOperation{Description: "list users"},
			},
			"/api/posts": {
				Post: &OpenAPIOperation{Description: "create post"},
			},
		},
	}
	added := MergeOpenAPIPaths(existing, incoming)
	if len(added) != 2 {
		t.Errorf("expected 2 added; got %d: %v", len(added), added)
	}
	paths, ok := existing["paths"].(map[string]interface{})
	if !ok {
		t.Fatalf("paths is not map[string]interface{}")
	}
	if _, ok := paths["/api/users"]; !ok {
		t.Error("expected /api/users to be in paths")
	}
	if _, ok := paths["/api/posts"]; !ok {
		t.Error("expected /api/posts to be in paths")
	}
}

// TestMergeOpenAPIPaths_ExistingPreserved verifies existing paths are not modified.
func TestMergeOpenAPIPaths_ExistingPreserved(t *testing.T) {
	t.Parallel()
	existing := map[string]interface{}{
		"openapi": "3.0.3",
		"paths": map[string]interface{}{
			"/api/users": map[string]interface{}{
				"get": map[string]interface{}{"description": "original"},
			},
		},
	}
	incoming := &OpenAPISpec{
		Paths: map[string]OpenAPIPathItem{
			"/api/users": {
				Get: &OpenAPIOperation{Description: "new version — must not overwrite"},
			},
		},
	}
	added := MergeOpenAPIPaths(existing, incoming)
	if len(added) != 0 {
		t.Errorf("expected 0 added; got %d: %v", len(added), added)
	}
	paths := existing["paths"].(map[string]interface{})
	pathItem := paths["/api/users"].(map[string]interface{})
	get := pathItem["get"].(map[string]interface{})
	if get["description"] != "original" {
		t.Errorf("expected original description to be preserved, got %v", get["description"])
	}
}

// TestMergeOpenAPIPaths_NewMethodOnExistingPath verifies a new method is added
// to an existing path without touching other methods.
func TestMergeOpenAPIPaths_NewMethodOnExistingPath(t *testing.T) {
	t.Parallel()
	existing := map[string]interface{}{
		"openapi": "3.0.3",
		"paths": map[string]interface{}{
			"/api/users": map[string]interface{}{
				"get": map[string]interface{}{"description": "list"},
			},
		},
	}
	incoming := &OpenAPISpec{
		Paths: map[string]OpenAPIPathItem{
			"/api/users": {
				Get:  &OpenAPIOperation{Description: "should not overwrite get"},
				Post: &OpenAPIOperation{Description: "create user"},
			},
		},
	}
	added := MergeOpenAPIPaths(existing, incoming)
	if len(added) != 1 {
		t.Errorf("expected 1 added; got %d: %v", len(added), added)
	}
	if !strings.Contains(added[0], "POST") {
		t.Errorf("expected added key to contain POST; got %q", added[0])
	}
	paths := existing["paths"].(map[string]interface{})
	pathItem := paths["/api/users"].(map[string]interface{})
	// Existing GET must be unchanged.
	get := pathItem["get"].(map[string]interface{})
	if get["description"] != "list" {
		t.Errorf("expected original GET description preserved; got %v", get["description"])
	}
	// New POST must exist.
	if _, ok := pathItem["post"]; !ok {
		t.Error("expected post method to be added")
	}
}

// TestSerializeMergedYAML verifies SerializeMergedYAML produces valid YAML.
func TestSerializeMergedYAML(t *testing.T) {
	t.Parallel()
	data := map[string]interface{}{
		"openapi": "3.0.3",
		"paths":   map[string]interface{}{},
	}
	b, err := SerializeMergedYAML(data)
	if err != nil {
		t.Fatalf("SerializeMergedYAML: %v", err)
	}
	var out map[string]interface{}
	if err := yaml.Unmarshal(b, &out); err != nil {
		t.Fatalf("yaml.Unmarshal round-trip: %v", err)
	}
	if out["openapi"] != "3.0.3" {
		t.Errorf("openapi: got %v", out["openapi"])
	}
}

// TestSerializeMergedJSON verifies SerializeMergedJSON produces valid indented JSON.
func TestSerializeMergedJSON(t *testing.T) {
	t.Parallel()
	data := map[string]interface{}{
		"swagger": "2.0",
		"paths":   map[string]interface{}{},
	}
	b, err := SerializeMergedJSON(data)
	if err != nil {
		t.Fatalf("SerializeMergedJSON: %v", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("json.Unmarshal round-trip: %v", err)
	}
	if out["swagger"] != "2.0" {
		t.Errorf("swagger: got %v", out["swagger"])
	}
}
