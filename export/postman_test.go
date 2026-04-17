package export

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/AgusRdz/probe/observer"
	"github.com/AgusRdz/probe/store"
)

// TestGeneratePostman_Empty verifies that an empty store produces a collection
// with zero items and the correct schema URL.
func TestGeneratePostman_Empty(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	col, err := GeneratePostman(s, defaultOpts())
	if err != nil {
		t.Fatalf("GeneratePostman: %v", err)
	}
	if col.Info.Schema != postmanSchemaURL {
		t.Errorf("schema URL: got %q, want %q", col.Info.Schema, postmanSchemaURL)
	}
	if col.Item == nil {
		t.Error("Item must not be nil for an empty store")
	}
	if len(col.Item) != 0 {
		t.Errorf("expected 0 items, got %d", len(col.Item))
	}
}

// TestGeneratePostman_SingleEndpoint verifies that one observed endpoint produces
// an item with the correct method and path.
func TestGeneratePostman_SingleEndpoint(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	pair := observer.CapturedPair{
		Method:          "POST",
		RawPath:         "/api/users",
		ReqContentType:  "application/json",
		RespContentType: "application/json",
		StatusCode:      201,
		LatencyMs:       10,
		ReqBody:         []byte(`{"name":"alice"}`),
		RespBody:        []byte(`{"id":1,"name":"alice"}`),
	}
	reqSchema := &observer.Schema{
		Type: "object",
		Properties: map[string]*observer.Schema{
			"name": {Type: "string"},
		},
	}
	respSchema := &observer.Schema{
		Type: "object",
		Properties: map[string]*observer.Schema{
			"id":   {Type: "integer"},
			"name": {Type: "string"},
		},
	}
	if err := s.Record(pair, reqSchema, respSchema); err != nil {
		t.Fatalf("store.Record: %v", err)
	}

	opts := defaultOpts()
	opts.InfoTitle = "My API"

	col, err := GeneratePostman(s, opts)
	if err != nil {
		t.Fatalf("GeneratePostman: %v", err)
	}
	if col.Info.Name != "My API" {
		t.Errorf("collection name: got %q, want %q", col.Info.Name, "My API")
	}
	if len(col.Item) != 1 {
		t.Fatalf("expected 1 item, got %d", len(col.Item))
	}

	item := col.Item[0]
	if item.Request.Method != "POST" {
		t.Errorf("method: got %q, want %q", item.Request.Method, "POST")
	}
	if !strings.Contains(item.Request.URL.Raw, "/api/users") {
		t.Errorf("URL raw: got %q, want to contain /api/users", item.Request.URL.Raw)
	}
}

// TestGeneratePostman_AuthHeader verifies that an endpoint with RequiresAuth=true
// gets "Authorization: Bearer {{token}}" in its Postman request headers.
func TestGeneratePostman_AuthHeader(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	_, err := s.UpsertScannedEndpoint(store.ScannedEndpointInput{
		Method:       "GET",
		PathPattern:  "api/users",
		Protocol:     "rest",
		RequiresAuth: true,
	})
	if err != nil {
		t.Fatalf("UpsertScannedEndpoint: %v", err)
	}

	col, err := GeneratePostman(s, defaultOpts())
	if err != nil {
		t.Fatalf("GeneratePostman: %v", err)
	}
	if len(col.Item) != 1 {
		t.Fatalf("expected 1 item, got %d", len(col.Item))
	}

	headers := col.Item[0].Request.Header
	var found bool
	for _, h := range headers {
		if strings.ToLower(h.Key) == "authorization" && h.Value == "Bearer {{token}}" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected Authorization: Bearer {{token}} header; got %+v", headers)
	}
}

// TestGeneratePostman_NoAuthForLoginPath verifies that a RequiresAuth=true endpoint
// whose path ends in "login" does NOT get an Authorization header injected.
func TestGeneratePostman_NoAuthForLoginPath(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	_, err := s.UpsertScannedEndpoint(store.ScannedEndpointInput{
		Method:       "POST",
		PathPattern:  "api/auth/login",
		Protocol:     "rest",
		RequiresAuth: true,
	})
	if err != nil {
		t.Fatalf("UpsertScannedEndpoint: %v", err)
	}

	col, err := GeneratePostman(s, defaultOpts())
	if err != nil {
		t.Fatalf("GeneratePostman: %v", err)
	}
	if len(col.Item) != 1 {
		t.Fatalf("expected 1 item, got %d", len(col.Item))
	}

	headers := col.Item[0].Request.Header
	for _, h := range headers {
		if strings.ToLower(h.Key) == "authorization" {
			t.Errorf("expected no Authorization header for login path; got %+v", h)
		}
	}
}

// TestWritePostman verifies that output is valid JSON containing "info.name".
func TestWritePostman(t *testing.T) {
	t.Parallel()
	col := &PostmanCollection{
		Info: PostmanInfo{
			Name:   "Test Collection",
			Schema: postmanSchemaURL,
		},
		Item: []PostmanItem{},
	}

	var buf bytes.Buffer
	if err := WritePostman(&buf, col); err != nil {
		t.Fatalf("WritePostman: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, buf.String())
	}

	info, ok := out["info"].(map[string]interface{})
	if !ok {
		t.Fatal("missing 'info' key in output")
	}
	name, _ := info["name"].(string)
	if name != "Test Collection" {
		t.Errorf("info.name: got %q, want %q", name, "Test Collection")
	}
}

// TestGeneratePostman_MultipleVariants verifies that an endpoint with two variants
// produces two separate Postman items (one per variant).
func TestGeneratePostman_MultipleVariants(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	pairPassword := observer.CapturedPair{
		Method: "POST", RawPath: "/api/login",
		ReqContentType: "application/json", StatusCode: 200,
	}
	schemaPassword := &observer.Schema{
		Type: "object",
		Properties: map[string]*observer.Schema{
			"username": {Type: "string"},
			"password": {Type: "string"},
		},
	}
	pairToken := observer.CapturedPair{
		Method: "POST", RawPath: "/api/login",
		ReqContentType: "application/json", StatusCode: 200,
	}
	schemaToken := &observer.Schema{
		Type: "object",
		Properties: map[string]*observer.Schema{
			"token": {Type: "string"},
		},
	}

	if err := s.Record(pairPassword, schemaPassword, nil); err != nil {
		t.Fatalf("Record password: %v", err)
	}
	if err := s.Record(pairToken, schemaToken, nil); err != nil {
		t.Fatalf("Record token: %v", err)
	}

	col, err := GeneratePostman(s, defaultOpts())
	if err != nil {
		t.Fatalf("GeneratePostman: %v", err)
	}

	if len(col.Item) != 2 {
		t.Fatalf("expected 2 Postman items (one per variant); got %d", len(col.Item))
	}

	// Both items should be POST /api/login but with different names.
	names := map[string]bool{}
	for _, item := range col.Item {
		names[item.Name] = true
		if item.Request.Method != "POST" {
			t.Errorf("expected POST; got %s", item.Request.Method)
		}
	}
	if len(names) != 2 {
		t.Errorf("expected 2 distinct item names; got %v", names)
	}
}
