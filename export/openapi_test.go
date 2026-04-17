package export

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AgusRdz/probe/observer"
	"github.com/AgusRdz/probe/store"
)

// newTestStore opens a fresh SQLite database in a temp directory for each test.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "probe_test.db")
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func defaultOpts() ExportOptions {
	return ExportOptions{
		MinCalls:            0,
		ConfidenceThreshold: 0.9,
		InfoTitle:           "Test API",
		InfoVersion:         "1.0.0",
	}
}

// TestGenerateOpenAPI_Empty verifies that an empty store produces a spec with
// an empty paths map rather than nil.
func TestGenerateOpenAPI_Empty(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	spec, _, err := GenerateOpenAPI(s, defaultOpts())
	if err != nil {
		t.Fatalf("GenerateOpenAPI: %v", err)
	}
	if spec.OpenAPI != "3.0.3" {
		t.Errorf("openapi version: got %q, want %q", spec.OpenAPI, "3.0.3")
	}
	if spec.Paths == nil {
		t.Error("Paths must not be nil for an empty store")
	}
	if len(spec.Paths) != 0 {
		t.Errorf("expected 0 paths, got %d", len(spec.Paths))
	}
}

// TestGenerateOpenAPI_SingleEndpoint verifies that one observed endpoint with
// field confidence produces a valid spec with the correct path, path parameters,
// and request body with required/optional fields.
func TestGenerateOpenAPI_SingleEndpoint(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	// Record two observations for POST /users/{id}.
	reqSchema := &observer.Schema{
		Type: "object",
		Properties: map[string]*observer.Schema{
			"name":  {Type: "string"},
			"email": {Type: "string", Format: "email"},
			"age":   {Type: "integer"},
		},
	}
	respSchema := &observer.Schema{
		Type: "object",
		Properties: map[string]*observer.Schema{
			"id": {Type: "string", Format: "uuid"},
		},
	}

	pair := observer.CapturedPair{
		Method:          "POST",
		RawPath:         "/users/42",
		ReqContentType:  "application/json",
		RespContentType: "application/json",
		StatusCode:      201,
		LatencyMs:       10,
	}

	if err := s.Record(pair, reqSchema, respSchema); err != nil {
		t.Fatalf("Record (1st): %v", err)
	}
	// Second observation — only name and email present (age absent).
	reqSchema2 := &observer.Schema{
		Type: "object",
		Properties: map[string]*observer.Schema{
			"name":  {Type: "string"},
			"email": {Type: "string", Format: "email"},
		},
	}
	if err := s.Record(pair, reqSchema2, respSchema); err != nil {
		t.Fatalf("Record (2nd): %v", err)
	}

	// Upsert the endpoint with the normalised path pattern so GetEndpoints returns it.
	_, err := s.UpsertEndpoint("POST", "/users/{id}", "rest", "observed")
	if err != nil {
		t.Fatalf("UpsertEndpoint: %v", err)
	}

	opts := defaultOpts()
	opts.ConfidenceThreshold = 0.9 // name/email seen 2/2=1.0 → required; age 1/2=0.5 → optional

	spec, _, err := GenerateOpenAPI(s, opts)
	if err != nil {
		t.Fatalf("GenerateOpenAPI: %v", err)
	}

	if len(spec.Paths) == 0 {
		t.Fatal("expected at least one path in spec")
	}

	// Find the /users/{id} path.
	pathItem, ok := spec.Paths["/users/{id}"]
	if !ok {
		t.Fatalf("path /users/{id} not found; paths: %v", keysOf(spec.Paths))
	}

	op := pathItem.Post
	if op == nil {
		t.Fatal("expected POST operation on /users/{id}")
	}

	// Verify path parameter extraction.
	if len(op.Parameters) != 1 {
		t.Fatalf("expected 1 path param, got %d", len(op.Parameters))
	}
	p := op.Parameters[0]
	if p.Name != "id" {
		t.Errorf("param name: got %q, want %q", p.Name, "id")
	}
	if p.In != "path" {
		t.Errorf("param in: got %q, want %q", p.In, "path")
	}
	if !p.Required {
		t.Error("path param must be required")
	}

	// Verify request body is present.
	if op.RequestBody == nil {
		t.Fatal("expected requestBody to be present")
	}

	mt, ok := op.RequestBody.Content["application/json"]
	if !ok {
		t.Fatal("expected application/json content type in requestBody")
	}

	schema := mt.Schema
	if schema.Type != "object" {
		t.Errorf("requestBody schema type: got %q, want object", schema.Type)
	}
	if len(schema.Properties) == 0 {
		t.Error("expected at least one property in requestBody schema")
	}
}

// TestWriteYAML verifies that the YAML output contains the openapi version
// declaration and the expected path key.
func TestWriteYAML(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	_, err := s.UpsertEndpoint("GET", "/health", "rest", "observed")
	if err != nil {
		t.Fatalf("UpsertEndpoint: %v", err)
	}

	spec, _, err := GenerateOpenAPI(s, defaultOpts())
	if err != nil {
		t.Fatalf("GenerateOpenAPI: %v", err)
	}

	var buf bytes.Buffer
	if err := WriteYAML(&buf, spec); err != nil {
		t.Fatalf("WriteYAML: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "openapi: 3.0.3") {
		t.Errorf("YAML output missing 'openapi: 3.0.3'; got:\n%s", out)
	}
	if !strings.Contains(out, "/health") {
		t.Errorf("YAML output missing path '/health'; got:\n%s", out)
	}
}

// TestWriteJSON verifies that the JSON output is valid and contains expected fields.
func TestWriteJSON(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	_, err := s.UpsertEndpoint("GET", "/ping", "rest", "observed")
	if err != nil {
		t.Fatalf("UpsertEndpoint: %v", err)
	}

	spec, _, err := GenerateOpenAPI(s, defaultOpts())
	if err != nil {
		t.Fatalf("GenerateOpenAPI: %v", err)
	}

	var buf bytes.Buffer
	if err := WriteJSON(&buf, spec); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("JSON output is not valid JSON: %v", err)
	}
	if decoded["openapi"] != "3.0.3" {
		t.Errorf("JSON openapi field: got %v, want 3.0.3", decoded["openapi"])
	}
}

// TestBuildSchemaFromConfidence verifies that dot-notation field paths produce
// correctly nested OpenAPISchema objects.
func TestBuildSchemaFromConfidence(t *testing.T) {
	t.Parallel()

	fields := []store.FieldConfidenceRow{
		// Top-level required field (3/3 = 1.0 confidence).
		{Location: "request", FieldPath: "name", SeenCount: 3, TotalCalls: 3, TypeJSON: `{"type":"string"}`},
		// Nested required field (3/3 = 1.0 confidence).
		{Location: "request", FieldPath: "address.street", SeenCount: 3, TotalCalls: 3, TypeJSON: `{"type":"string"}`},
		// Nested optional field (1/3 ≈ 0.33 confidence).
		{Location: "request", FieldPath: "address.city", SeenCount: 1, TotalCalls: 3, TypeJSON: `{"type":"string"}`},
		// Deeply nested field (3/3 = 1.0 confidence).
		{Location: "request", FieldPath: "address.geo.lat", SeenCount: 3, TotalCalls: 3, TypeJSON: `{"type":"number"}`},
		// Response field — must be ignored.
		{Location: "response", FieldPath: "id", SeenCount: 3, TotalCalls: 3, TypeJSON: `{"type":"string"}`},
	}

	schema := buildSchemaFromConfidence(fields, "request", 0.9)
	if schema == nil {
		t.Fatal("expected non-nil schema")
	}
	if schema.Type != "object" {
		t.Errorf("root type: got %q, want object", schema.Type)
	}

	// "name" must be present and required.
	if _, ok := schema.Properties["name"]; !ok {
		t.Error("expected 'name' property at root")
	}
	if !contains(schema.Required, "name") {
		t.Error("expected 'name' in root required list")
	}

	// "address" must be a nested object.
	addrSchema, ok := schema.Properties["address"]
	if !ok {
		t.Fatal("expected 'address' property at root")
	}
	if addrSchema.Type != "object" {
		t.Errorf("address type: got %q, want object", addrSchema.Type)
	}

	// "address.street" required, "address.city" optional.
	if _, ok := addrSchema.Properties["street"]; !ok {
		t.Error("expected 'street' inside address")
	}
	if !contains(addrSchema.Required, "street") {
		t.Error("expected 'street' in address.required")
	}
	if contains(addrSchema.Required, "city") {
		t.Error("'city' should NOT be required (confidence < threshold)")
	}

	// "address.geo" must be a further nested object with "lat".
	geoSchema, ok := addrSchema.Properties["geo"]
	if !ok {
		t.Fatal("expected 'geo' inside address")
	}
	if _, ok := geoSchema.Properties["lat"]; !ok {
		t.Error("expected 'lat' inside address.geo")
	}
	if !contains(geoSchema.Required, "lat") {
		t.Error("expected 'lat' in address.geo.required")
	}

	// Response field must not appear in the request schema.
	if _, ok := schema.Properties["id"]; ok {
		t.Error("response field 'id' must not appear in request schema")
	}
}

// --- helpers ---

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func keysOf(m map[string]OpenAPIPathItem) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
