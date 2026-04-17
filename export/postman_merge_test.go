package export

import (
	"encoding/json"
	"strings"
	"testing"
)

func postmanItemJSON(method, rawURL, bodyRaw string) json.RawMessage {
	item := map[string]interface{}{
		"name": method + " " + rawURL,
		"request": map[string]interface{}{
			"method": method,
			"header": []interface{}{},
			"url": map[string]interface{}{
				"raw":  rawURL,
				"host": []string{"{{baseUrl}}"},
				"path": strings.Split(strings.TrimPrefix(rawURL, "{{baseUrl}}/"), "/"),
			},
		},
	}
	if bodyRaw != "" {
		item["request"].(map[string]interface{})["body"] = map[string]interface{}{
			"mode": "raw",
			"raw":  bodyRaw,
		}
	}
	b, _ := json.Marshal(item)
	return b
}

func makeExistingCollection(items ...json.RawMessage) *ExistingCollection {
	return &ExistingCollection{
		Info:  PostmanInfo{Name: "Test", Schema: postmanSchemaURL},
		Items: items,
	}
}

// TestExistingItemKey verifies path normalization from Postman {{param}} to {param}.
func TestExistingItemKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		rawURL  string
		method  string
		wantKey string
	}{
		{"{{baseUrl}}/api/users", "GET", "GET /api/users"},
		{"{{baseUrl}}/api/users/{{id}}", "GET", "GET /api/users/{id}"},
		{"{{baseUrl}}/api/users/{{id}}/posts/{{postId}}", "DELETE", "DELETE /api/users/{id}/posts/{postId}"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.wantKey, func(t *testing.T) {
			t.Parallel()
			raw := postmanItemJSON(tc.method, tc.rawURL, "")
			key, _, err := ExistingItemKey(raw)
			if err != nil {
				t.Fatalf("ExistingItemKey: %v", err)
			}
			if key != tc.wantKey {
				t.Errorf("got %q; want %q", key, tc.wantKey)
			}
		})
	}
}

// TestComputeMerge_AllNew verifies that when none of the incoming items exist,
// all are added with no conflicts.
func TestComputeMerge_AllNew(t *testing.T) {
	t.Parallel()

	existing := makeExistingCollection(
		postmanItemJSON("GET", "{{baseUrl}}/api/users", ""),
	)
	incoming := &PostmanCollection{
		Item: []PostmanItem{
			{
				Name: "POST /api/users",
				Request: PostmanRequest{
					Method: "POST",
					URL:    PostmanURL{Raw: "{{baseUrl}}/api/users"},
				},
			},
			{
				Name: "DELETE /api/users/{id}",
				Request: PostmanRequest{
					Method: "DELETE",
					URL:    PostmanURL{Raw: "{{baseUrl}}/api/users/{{id}}"},
				},
			},
		},
	}

	result, err := ComputeMerge(existing, incoming)
	if err != nil {
		t.Fatalf("ComputeMerge: %v", err)
	}
	if len(result.Added) != 2 {
		t.Errorf("expected 2 added; got %d", len(result.Added))
	}
	if len(result.Conflicts) != 0 {
		t.Errorf("expected 0 conflicts; got %d", len(result.Conflicts))
	}
}

// TestComputeMerge_NoConflict verifies that when incoming matches existing exactly,
// nothing is added and there are no conflicts.
func TestComputeMerge_NoConflict(t *testing.T) {
	t.Parallel()

	body := `{"name":""}`
	existing := makeExistingCollection(
		postmanItemJSON("POST", "{{baseUrl}}/api/users", body),
	)
	incoming := &PostmanCollection{
		Item: []PostmanItem{
			{
				Name: "POST /api/users",
				Request: PostmanRequest{
					Method: "POST",
					URL:    PostmanURL{Raw: "{{baseUrl}}/api/users"},
					Body:   &PostmanBody{Mode: "raw", Raw: body},
				},
			},
		},
	}

	result, err := ComputeMerge(existing, incoming)
	if err != nil {
		t.Fatalf("ComputeMerge: %v", err)
	}
	if len(result.Added) != 0 {
		t.Errorf("expected 0 added; got %d", len(result.Added))
	}
	if len(result.Conflicts) != 0 {
		t.Errorf("expected 0 conflicts; got %d", len(result.Conflicts))
	}
	if len(result.Unchanged) != 1 {
		t.Errorf("expected 1 unchanged; got %d", len(result.Unchanged))
	}
}

// TestComputeMerge_Conflict verifies that when the incoming body differs from
// the existing body, a conflict is detected.
func TestComputeMerge_Conflict(t *testing.T) {
	t.Parallel()

	existing := makeExistingCollection(
		postmanItemJSON("POST", "{{baseUrl}}/api/users", `{"name":""}`),
	)
	incoming := &PostmanCollection{
		Item: []PostmanItem{
			{
				Name: "POST /api/users",
				Request: PostmanRequest{
					Method: "POST",
					URL:    PostmanURL{Raw: "{{baseUrl}}/api/users"},
					Body:   &PostmanBody{Mode: "raw", Raw: `{"name":"","email":""}`},
				},
			},
		},
	}

	result, err := ComputeMerge(existing, incoming)
	if err != nil {
		t.Fatalf("ComputeMerge: %v", err)
	}
	if len(result.Conflicts) != 1 {
		t.Fatalf("expected 1 conflict; got %d", len(result.Conflicts))
	}
	if result.Conflicts[0].Key != "POST /api/users" {
		t.Errorf("conflict key: got %q; want %q", result.Conflicts[0].Key, "POST /api/users")
	}
}

// TestBuildMergedCollection_Keep verifies that a "keep" resolution preserves
// the existing item verbatim (including unknown fields).
func TestBuildMergedCollection_Keep(t *testing.T) {
	t.Parallel()

	rawItem := postmanItemJSON("POST", "{{baseUrl}}/api/users", `{"name":""}`)
	// Add a custom field to simulate Postman scripts.
	var m map[string]interface{}
	_ = json.Unmarshal(rawItem, &m)
	m["event"] = []map[string]string{{"listen": "prerequest"}}
	rawItem, _ = json.Marshal(m)

	existing := makeExistingCollection(rawItem)
	incoming := &PostmanCollection{
		Item: []PostmanItem{
			{
				Name: "POST /api/users",
				Request: PostmanRequest{
					Method: "POST",
					URL:    PostmanURL{Raw: "{{baseUrl}}/api/users"},
					Body:   &PostmanBody{Mode: "raw", Raw: `{"name":"","email":""}`},
				},
			},
		},
	}

	result, err := ComputeMerge(existing, incoming)
	if err != nil {
		t.Fatalf("ComputeMerge: %v", err)
	}

	resolutions := map[string]string{"POST /api/users": "keep"}
	out, err := BuildMergedCollection(existing, incoming, result, resolutions)
	if err != nil {
		t.Fatalf("BuildMergedCollection: %v", err)
	}

	// The output should contain the "event" field (preserved from existing).
	if !strings.Contains(string(out), "event") {
		t.Error("expected 'event' field to be preserved in kept item")
	}
	// The output should NOT contain "email" (incoming body not applied).
	if strings.Contains(string(out), "email") {
		t.Error("expected 'email' field to be absent when keeping existing item")
	}
}

// TestBuildMergedCollection_Replace verifies that a "replace" resolution uses
// the incoming item.
func TestBuildMergedCollection_Replace(t *testing.T) {
	t.Parallel()

	existing := makeExistingCollection(
		postmanItemJSON("POST", "{{baseUrl}}/api/users", `{"name":""}`),
	)
	incoming := &PostmanCollection{
		Item: []PostmanItem{
			{
				Name: "POST /api/users",
				Request: PostmanRequest{
					Method: "POST",
					URL:    PostmanURL{Raw: "{{baseUrl}}/api/users"},
					Body:   &PostmanBody{Mode: "raw", Raw: `{"name":"","email":""}`},
				},
			},
		},
	}

	result, err := ComputeMerge(existing, incoming)
	if err != nil {
		t.Fatalf("ComputeMerge: %v", err)
	}

	resolutions := map[string]string{"POST /api/users": "replace"}
	out, err := BuildMergedCollection(existing, incoming, result, resolutions)
	if err != nil {
		t.Fatalf("BuildMergedCollection: %v", err)
	}

	if !strings.Contains(string(out), "email") {
		t.Error("expected 'email' field from incoming item after replace")
	}
}

// TestBuildMergedCollection_FieldMerge verifies that a "merge" resolution adds
// missing body fields from incoming without removing existing ones.
func TestBuildMergedCollection_FieldMerge(t *testing.T) {
	t.Parallel()

	existing := makeExistingCollection(
		postmanItemJSON("POST", "{{baseUrl}}/api/users", `{"name":"alice","custom":"kept"}`),
	)
	incoming := &PostmanCollection{
		Item: []PostmanItem{
			{
				Name: "POST /api/users",
				Request: PostmanRequest{
					Method: "POST",
					URL:    PostmanURL{Raw: "{{baseUrl}}/api/users"},
					Body:   &PostmanBody{Mode: "raw", Raw: `{"name":"","email":""}`},
				},
			},
		},
	}

	result, err := ComputeMerge(existing, incoming)
	if err != nil {
		t.Fatalf("ComputeMerge: %v", err)
	}

	resolutions := map[string]string{"POST /api/users": "merge"}
	out, err := BuildMergedCollection(existing, incoming, result, resolutions)
	if err != nil {
		t.Fatalf("BuildMergedCollection: %v", err)
	}

	outStr := string(out)
	// Existing fields preserved.
	if !strings.Contains(outStr, "custom") {
		t.Error("expected 'custom' field from existing item to be preserved")
	}
	// Incoming new field added.
	if !strings.Contains(outStr, "email") {
		t.Error("expected 'email' field from incoming item to be added")
	}
}

// TestBuildMergedCollection_AddedItems verifies that new items are appended.
func TestBuildMergedCollection_AddedItems(t *testing.T) {
	t.Parallel()

	existing := makeExistingCollection(
		postmanItemJSON("GET", "{{baseUrl}}/api/users", ""),
	)
	incoming := &PostmanCollection{
		Item: []PostmanItem{
			{
				Name: "GET /api/users",
				Request: PostmanRequest{
					Method: "GET",
					URL:    PostmanURL{Raw: "{{baseUrl}}/api/users"},
				},
			},
			{
				Name: "POST /api/users",
				Request: PostmanRequest{
					Method: "POST",
					URL:    PostmanURL{Raw: "{{baseUrl}}/api/users"},
					Body:   &PostmanBody{Mode: "raw", Raw: `{"name":""}`},
				},
			},
		},
	}

	result, err := ComputeMerge(existing, incoming)
	if err != nil {
		t.Fatalf("ComputeMerge: %v", err)
	}

	out, err := BuildMergedCollection(existing, incoming, result, nil)
	if err != nil {
		t.Fatalf("BuildMergedCollection: %v", err)
	}

	// Decode the output and verify item count.
	var col map[string]interface{}
	if err := json.Unmarshal(out, &col); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	items := col["item"].([]interface{})
	if len(items) != 2 {
		t.Errorf("expected 2 items in merged collection; got %d", len(items))
	}
}
