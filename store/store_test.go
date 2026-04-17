package store

import (
	"path/filepath"
	"testing"

	"github.com/AgusRdz/probe/observer"
)

// newTestStore opens a fresh SQLite database in a temp directory for each test.
// t.TempDir() is cleaned up automatically; no ghost files left in the repo.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "probe_test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpen(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	if s.db == nil {
		t.Fatal("expected non-nil db")
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Mark already-closed so cleanup doesn't double-close.
	s.db = nil
}

func TestUpsertEndpointIdempotent(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	id1, err := s.UpsertEndpoint("GET", "/api/users", "rest", "observed")
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	id2, err := s.UpsertEndpoint("GET", "/api/users", "rest", "observed")
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	if id1 != id2 {
		t.Errorf("expected same ID on duplicate upsert; got %d and %d", id1, id2)
	}
}

func TestRecord(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	pair := observer.CapturedPair{
		Method:          "POST",
		RawPath:         "/api/items",
		ReqContentType:  "application/json",
		RespContentType: "application/json",
		StatusCode:      201,
		LatencyMs:       12,
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
		t.Fatalf("Record: %v", err)
	}

	endpoints, err := s.GetEndpoints()
	if err != nil {
		t.Fatalf("GetEndpoints: %v", err)
	}
	if len(endpoints) != 1 {
		t.Fatalf("expected 1 endpoint; got %d", len(endpoints))
	}
	if endpoints[0].CallCount != 1 {
		t.Errorf("expected call_count=1; got %d", endpoints[0].CallCount)
	}
	if endpoints[0].Method != "POST" {
		t.Errorf("expected method=POST; got %q", endpoints[0].Method)
	}
}

func TestFieldConfidenceTracking(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	pair := observer.CapturedPair{
		Method:     "GET",
		RawPath:    "/api/users",
		StatusCode: 200,
	}

	// Schema WITH "email" field.
	withEmail := &observer.Schema{
		Type: "object",
		Properties: map[string]*observer.Schema{
			"id":    {Type: "integer"},
			"email": {Type: "string", Format: "email"},
		},
	}
	// Schema WITHOUT "email" field.
	withoutEmail := &observer.Schema{
		Type: "object",
		Properties: map[string]*observer.Schema{
			"id": {Type: "integer"},
		},
	}

	// Call 1: with email.
	if err := s.Record(pair, nil, withEmail); err != nil {
		t.Fatalf("Record call 1: %v", err)
	}
	// Call 2: without email.
	if err := s.Record(pair, nil, withoutEmail); err != nil {
		t.Fatalf("Record call 2: %v", err)
	}
	// Call 3: with email.
	if err := s.Record(pair, nil, withEmail); err != nil {
		t.Fatalf("Record call 3: %v", err)
	}

	endpoints, err := s.GetEndpoints()
	if err != nil {
		t.Fatalf("GetEndpoints: %v", err)
	}
	if len(endpoints) == 0 {
		t.Fatal("no endpoints")
	}

	rows, err := s.GetFieldConfidence(endpoints[0].ID)
	if err != nil {
		t.Fatalf("GetFieldConfidence: %v", err)
	}

	confidence := map[string]struct{ seen, total int }{}
	for _, r := range rows {
		if r.Location == "response" {
			confidence[r.FieldPath] = struct{ seen, total int }{r.SeenCount, r.TotalCalls}
		}
	}

	emailConf, ok := confidence["email"]
	if !ok {
		t.Fatal("expected field_confidence row for 'email'")
	}
	if emailConf.seen != 2 {
		t.Errorf("email seen_count: want 2, got %d", emailConf.seen)
	}
	if emailConf.total != 3 {
		t.Errorf("email total_calls: want 3, got %d", emailConf.total)
	}

	idConf, ok := confidence["id"]
	if !ok {
		t.Fatal("expected field_confidence row for 'id'")
	}
	if idConf.seen != 3 {
		t.Errorf("id seen_count: want 3, got %d", idConf.seen)
	}
	if idConf.total != 3 {
		t.Errorf("id total_calls: want 3, got %d", idConf.total)
	}
}

func TestDeleteEndpoint(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	pair := observer.CapturedPair{
		Method:     "DELETE",
		RawPath:    "/api/things/1",
		StatusCode: 204,
	}
	if err := s.Record(pair, nil, nil); err != nil {
		t.Fatalf("Record: %v", err)
	}

	endpoints, err := s.GetEndpoints()
	if err != nil {
		t.Fatalf("GetEndpoints: %v", err)
	}
	if len(endpoints) != 1 {
		t.Fatalf("expected 1 endpoint before delete; got %d", len(endpoints))
	}

	if err := s.DeleteEndpoint(endpoints[0].ID); err != nil {
		t.Fatalf("DeleteEndpoint: %v", err)
	}

	endpoints, err = s.GetEndpoints()
	if err != nil {
		t.Fatalf("GetEndpoints after delete: %v", err)
	}
	if len(endpoints) != 0 {
		t.Errorf("expected 0 endpoints after delete; got %d", len(endpoints))
	}
}

func TestStats(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	// Two distinct endpoints, both observed.
	pairs := []observer.CapturedPair{
		{Method: "GET", RawPath: "/api/a", StatusCode: 200},
		{Method: "POST", RawPath: "/api/b", StatusCode: 201},
	}
	for _, p := range pairs {
		if err := s.Record(p, nil, nil); err != nil {
			t.Fatalf("Record %s %s: %v", p.Method, p.RawPath, err)
		}
	}

	stats, err := s.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}

	if got := stats["total"]; got != 2 {
		t.Errorf("total = %d; want 2", got)
	}
	if got := stats["observed"]; got != 2 {
		t.Errorf("observed = %d; want 2", got)
	}
}

func TestRecordCapturesHeaders(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	pair := observer.CapturedPair{
		Method:     "GET",
		RawPath:    "/api/users",
		StatusCode: 200,
		ReqHeaders: []string{"Authorization", "Accept"},
	}

	if err := s.Record(pair, nil, nil); err != nil {
		t.Fatalf("Record: %v", err)
	}

	endpoints, err := s.GetEndpoints()
	if err != nil {
		t.Fatalf("GetEndpoints: %v", err)
	}
	if len(endpoints) != 1 {
		t.Fatalf("expected 1 endpoint; got %d", len(endpoints))
	}

	headers, err := s.GetEndpointHeaders(endpoints[0].ID)
	if err != nil {
		t.Fatalf("GetEndpointHeaders: %v", err)
	}

	headerMap := map[string]HeaderRow{}
	for _, h := range headers {
		headerMap[h.HeaderName] = h
	}

	for _, want := range []string{"Authorization", "Accept"} {
		h, ok := headerMap[want]
		if !ok {
			t.Errorf("expected header %q in result", want)
			continue
		}
		if h.SeenCount != 1 {
			t.Errorf("header %q seen_count: want 1, got %d", want, h.SeenCount)
		}
		if h.TotalCalls != 1 {
			t.Errorf("header %q total_calls: want 1, got %d", want, h.TotalCalls)
		}
	}
}

func TestRecordCapturesQueryParams(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	pair := observer.CapturedPair{
		Method:     "GET",
		RawPath:    "/api/users?page=1&limit=10",
		StatusCode: 200,
	}

	if err := s.Record(pair, nil, nil); err != nil {
		t.Fatalf("Record: %v", err)
	}

	endpoints, err := s.GetEndpoints()
	if err != nil {
		t.Fatalf("GetEndpoints: %v", err)
	}
	if len(endpoints) != 1 {
		t.Fatalf("expected 1 endpoint; got %d", len(endpoints))
	}

	params, err := s.GetQueryParams(endpoints[0].ID)
	if err != nil {
		t.Fatalf("GetQueryParams: %v", err)
	}

	paramMap := map[string]QueryParamRow{}
	for _, p := range params {
		paramMap[p.ParamName] = p
	}

	for _, want := range []string{"page", "limit"} {
		if _, ok := paramMap[want]; !ok {
			t.Errorf("expected query param %q in result", want)
		}
	}
}
