package observer

import (
	"testing"
)

func TestConfidence_Ratio(t *testing.T) {
	f := FieldConfidence{SeenCount: 2, TotalCalls: 3}
	got := f.Confidence()
	want := 2.0 / 3.0
	if got != want {
		t.Errorf("Confidence() = %v, want %v", got, want)
	}
}

func TestConfidence_ZeroTotalCalls(t *testing.T) {
	f := FieldConfidence{SeenCount: 5, TotalCalls: 0}
	if got := f.Confidence(); got != 0 {
		t.Errorf("Confidence() = %v, want 0 when TotalCalls == 0", got)
	}
}

func TestIsRequired_BelowThreshold(t *testing.T) {
	// 2/3 ≈ 0.667, below 0.9
	f := FieldConfidence{SeenCount: 2, TotalCalls: 3}
	if f.IsRequired(0.9) {
		t.Error("IsRequired(0.9) = true, want false for confidence 0.67")
	}
}

func TestIsRequired_AtThreshold(t *testing.T) {
	// 3/3 = 1.0, above 0.9
	f := FieldConfidence{SeenCount: 3, TotalCalls: 3}
	if !f.IsRequired(0.9) {
		t.Error("IsRequired(0.9) = false, want true for confidence 1.0")
	}
}

func TestOverallConfidence_Average(t *testing.T) {
	fields := []FieldConfidence{
		{Location: "response", SeenCount: 3, TotalCalls: 3}, // 1.0
		{Location: "response", SeenCount: 1, TotalCalls: 2}, // 0.5
		{Location: "response", SeenCount: 2, TotalCalls: 4}, // 0.5
	}
	// average = (1.0 + 0.5 + 0.5) / 3 = 0.6667
	got := OverallConfidence(fields, "response")
	want := (1.0 + 0.5 + 0.5) / 3.0
	if got != want {
		t.Errorf("OverallConfidence() = %v, want %v", got, want)
	}
}

func TestOverallConfidence_FiltersByLocation(t *testing.T) {
	fields := []FieldConfidence{
		{Location: "request", SeenCount: 1, TotalCalls: 1},  // 1.0
		{Location: "response", SeenCount: 1, TotalCalls: 2}, // 0.5
		{Location: "response", SeenCount: 1, TotalCalls: 4}, // 0.25
	}
	got := OverallConfidence(fields, "response")
	want := (0.5 + 0.25) / 2.0
	if got != want {
		t.Errorf("OverallConfidence(response) = %v, want %v", got, want)
	}

	gotReq := OverallConfidence(fields, "request")
	wantReq := 1.0
	if gotReq != wantReq {
		t.Errorf("OverallConfidence(request) = %v, want %v", gotReq, wantReq)
	}
}

func TestOverallConfidence_NoFields(t *testing.T) {
	if got := OverallConfidence(nil, "response"); got != 0 {
		t.Errorf("OverallConfidence(nil) = %v, want 0", got)
	}
}

func TestOverallConfidence_NoMatchingLocation(t *testing.T) {
	fields := []FieldConfidence{
		{Location: "request", SeenCount: 1, TotalCalls: 1},
	}
	if got := OverallConfidence(fields, "response"); got != 0 {
		t.Errorf("OverallConfidence with no matching location = %v, want 0", got)
	}
}
