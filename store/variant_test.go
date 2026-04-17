package store

import (
	"strings"
	"testing"

	"github.com/AgusRdz/probe/observer"
)

// --- computeVariantFingerprint ---

func TestComputeVariantFingerprintAuthSchemes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		headers     []string
		wantPrefix  string
	}{
		{"bearer token", []string{"Authorization"}, "auth:bearer|"},
		{"no auth", []string{}, "auth:none|"},
		{"other header only", []string{"Accept"}, "auth:none|"},
	}

	// Simulate a bearer Authorization header value being present by using
	// a pair whose ReqHeaders contains "Authorization" and a known bearer value.
	// computeVariantFingerprint reads header names only; auth scheme detection
	// needs the header value — we pass it via a separate authScheme helper.
	// Since the function operates on CapturedPair.ReqHeaders (names only),
	// we encode the auth scheme as part of the header name token
	// "Authorization:bearer", "Authorization:basic", or "Authorization" (no value).
	//
	// Adjust based on actual implementation contract.
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pair := observer.CapturedPair{ReqHeaders: tc.headers}
			fp := computeVariantFingerprint(pair, nil)
			if !strings.HasPrefix(fp, tc.wantPrefix) {
				t.Errorf("fingerprint %q does not start with %q", fp, tc.wantPrefix)
			}
		})
	}
}

func TestComputeVariantFingerprintBodyFields(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		schema  *observer.Schema
		wantFP  string
	}{
		{
			name:   "nil schema",
			schema: nil,
			wantFP: "auth:none|body:",
		},
		{
			name: "single field",
			schema: &observer.Schema{
				Type: "object",
				Properties: map[string]*observer.Schema{
					"username": {Type: "string"},
				},
			},
			wantFP: "auth:none|body:username",
		},
		{
			name: "multiple fields sorted",
			schema: &observer.Schema{
				Type: "object",
				Properties: map[string]*observer.Schema{
					"password": {Type: "string"},
					"username": {Type: "string"},
				},
			},
			wantFP: "auth:none|body:password,username",
		},
		{
			name: "token-only body",
			schema: &observer.Schema{
				Type: "object",
				Properties: map[string]*observer.Schema{
					"token": {Type: "string"},
				},
			},
			wantFP: "auth:none|body:token",
		},
		{
			name:   "non-object schema",
			schema: &observer.Schema{Type: "string"},
			wantFP: "auth:none|body:",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pair := observer.CapturedPair{}
			fp := computeVariantFingerprint(pair, tc.schema)
			if fp != tc.wantFP {
				t.Errorf("got %q, want %q", fp, tc.wantFP)
			}
		})
	}
}

func TestComputeVariantFingerprintDifferentSchemesSameBody(t *testing.T) {
	t.Parallel()
	schema := &observer.Schema{
		Type: "object",
		Properties: map[string]*observer.Schema{
			"data": {Type: "string"},
		},
	}

	pairBearer := observer.CapturedPair{ReqHeaders: []string{"Authorization:bearer"}}
	pairNone := observer.CapturedPair{}

	fpBearer := computeVariantFingerprint(pairBearer, schema)
	fpNone := computeVariantFingerprint(pairNone, schema)

	if fpBearer == fpNone {
		t.Errorf("expected different fingerprints for bearer vs no-auth; both got %q", fpBearer)
	}
}

// --- upsertVariantTx / GetVariants ---

func TestGetVariantsEmpty(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	id, err := s.UpsertEndpoint("POST", "/api/login", "rest", "observed")
	if err != nil {
		t.Fatalf("UpsertEndpoint: %v", err)
	}

	variants, err := s.GetVariants(id)
	if err != nil {
		t.Fatalf("GetVariants: %v", err)
	}
	if len(variants) != 0 {
		t.Errorf("expected 0 variants; got %d", len(variants))
	}
}

func TestRecordCreatesVariant(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	pair := observer.CapturedPair{
		Method:         "POST",
		RawPath:        "/api/login",
		ReqContentType: "application/json",
		StatusCode:     200,
	}
	schema := &observer.Schema{
		Type: "object",
		Properties: map[string]*observer.Schema{
			"username": {Type: "string"},
			"password": {Type: "string"},
		},
	}

	if err := s.Record(pair, schema, nil); err != nil {
		t.Fatalf("Record: %v", err)
	}

	eps, err := s.GetEndpoints()
	if err != nil {
		t.Fatalf("GetEndpoints: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("expected 1 endpoint; got %d", len(eps))
	}

	variants, err := s.GetVariants(eps[0].ID)
	if err != nil {
		t.Fatalf("GetVariants: %v", err)
	}
	if len(variants) != 1 {
		t.Fatalf("expected 1 variant after first Record; got %d", len(variants))
	}
	if variants[0].CallCount != 1 {
		t.Errorf("expected call_count=1; got %d", variants[0].CallCount)
	}
}

func TestRecordSameFingerprintIncrements(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	pair := observer.CapturedPair{
		Method:         "POST",
		RawPath:        "/api/login",
		ReqContentType: "application/json",
		StatusCode:     200,
	}
	schema := &observer.Schema{
		Type: "object",
		Properties: map[string]*observer.Schema{
			"username": {Type: "string"},
			"password": {Type: "string"},
		},
	}

	for i := 0; i < 3; i++ {
		if err := s.Record(pair, schema, nil); err != nil {
			t.Fatalf("Record[%d]: %v", i, err)
		}
	}

	eps, _ := s.GetEndpoints()
	variants, err := s.GetVariants(eps[0].ID)
	if err != nil {
		t.Fatalf("GetVariants: %v", err)
	}
	if len(variants) != 1 {
		t.Fatalf("expected 1 variant; got %d", len(variants))
	}
	if variants[0].CallCount != 3 {
		t.Errorf("expected call_count=3; got %d", variants[0].CallCount)
	}
}

func TestRecordDifferentFingerprintsCreatesTwoVariants(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	pairPassword := observer.CapturedPair{
		Method:         "POST",
		RawPath:        "/api/login",
		ReqContentType: "application/json",
		StatusCode:     200,
	}
	schemaPassword := &observer.Schema{
		Type: "object",
		Properties: map[string]*observer.Schema{
			"username": {Type: "string"},
			"password": {Type: "string"},
		},
	}

	pairToken := observer.CapturedPair{
		Method:         "POST",
		RawPath:        "/api/login",
		ReqContentType: "application/json",
		StatusCode:     200,
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

	eps, _ := s.GetEndpoints()
	variants, err := s.GetVariants(eps[0].ID)
	if err != nil {
		t.Fatalf("GetVariants: %v", err)
	}
	if len(variants) != 2 {
		t.Fatalf("expected 2 variants; got %d", len(variants))
	}
}

// --- GetVariantFieldConfidence ---

func TestGetVariantFieldConfidence(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	pair := observer.CapturedPair{
		Method:         "POST",
		RawPath:        "/api/login",
		ReqContentType: "application/json",
		StatusCode:     200,
	}
	schema := &observer.Schema{
		Type: "object",
		Properties: map[string]*observer.Schema{
			"username": {Type: "string"},
			"password": {Type: "string"},
		},
	}

	if err := s.Record(pair, schema, nil); err != nil {
		t.Fatalf("Record: %v", err)
	}

	eps, _ := s.GetEndpoints()
	variants, _ := s.GetVariants(eps[0].ID)
	if len(variants) == 0 {
		t.Fatal("expected at least 1 variant")
	}

	rows, err := s.GetVariantFieldConfidence(variants[0].ID)
	if err != nil {
		t.Fatalf("GetVariantFieldConfidence: %v", err)
	}

	fieldMap := map[string]FieldConfidenceRow{}
	for _, r := range rows {
		fieldMap[r.FieldPath] = r
	}

	for _, field := range []string{"username", "password"} {
		r, ok := fieldMap[field]
		if !ok {
			t.Errorf("field %q not found in variant_field_confidence", field)
			continue
		}
		if r.SeenCount != 1 {
			t.Errorf("field %q: expected seen_count=1; got %d", field, r.SeenCount)
		}
		if r.TotalCalls != 1 {
			t.Errorf("field %q: expected total_calls=1; got %d", field, r.TotalCalls)
		}
	}
}

// --- autoVariantLabel ---

func TestAutoVariantLabel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		fingerprint string
		wantLabel   string
	}{
		{"auth:bearer|body:data", "authenticated"},
		{"auth:basic|body:", "basic-auth"},
		{"auth:apikey|body:x", "api-key"},
		{"auth:none|body:token", "token-login"},
		{"auth:none|body:password,username", "password-login"},
		{"auth:none|body:username,password", "password-login"},
		{"auth:none|body:email,password", "password-login"},
		{"auth:none|body:something,else", ""},
		{"auth:none|body:", ""},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.fingerprint, func(t *testing.T) {
			t.Parallel()
			got := autoVariantLabel(tc.fingerprint)
			if got != tc.wantLabel {
				t.Errorf("autoVariantLabel(%q) = %q; want %q", tc.fingerprint, got, tc.wantLabel)
			}
		})
	}
}

// TestUpdateVariantLabel verifies that UpdateVariantLabel persists the label.
func TestUpdateVariantLabel(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	pair := observer.CapturedPair{
		Method:         "POST",
		RawPath:        "/api/login",
		ReqContentType: "application/json",
		StatusCode:     200,
	}
	schema := &observer.Schema{
		Type:       "object",
		Properties: map[string]*observer.Schema{"token": {Type: "string"}},
	}
	if err := s.Record(pair, schema, nil); err != nil {
		t.Fatalf("Record: %v", err)
	}

	eps, _ := s.GetEndpoints()
	variants, _ := s.GetVariants(eps[0].ID)
	if len(variants) == 0 {
		t.Fatal("expected at least 1 variant")
	}

	if err := s.UpdateVariantLabel(variants[0].ID, "sso"); err != nil {
		t.Fatalf("UpdateVariantLabel: %v", err)
	}

	variants, _ = s.GetVariants(eps[0].ID)
	if variants[0].Label != "sso" {
		t.Errorf("expected label %q; got %q", "sso", variants[0].Label)
	}
}

// TestRecordAutoLabelsVariant verifies that auto-labeling fires on insert.
func TestRecordAutoLabelsVariant(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)

	// Bearer auth → "authenticated"
	pair := observer.CapturedPair{
		Method:         "GET",
		RawPath:        "/api/profile",
		ReqContentType: "application/json",
		StatusCode:     200,
		ReqHeaders:     []string{"Authorization:bearer"},
	}
	if err := s.Record(pair, nil, nil); err != nil {
		t.Fatalf("Record: %v", err)
	}

	eps, _ := s.GetEndpoints()
	variants, _ := s.GetVariants(eps[0].ID)
	if len(variants) == 0 {
		t.Fatal("expected 1 variant")
	}
	if variants[0].Label != "authenticated" {
		t.Errorf("expected auto-label %q; got %q", "authenticated", variants[0].Label)
	}
}
