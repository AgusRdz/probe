package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AgusRdz/probe/config"
)

func TestLaravelDetect(t *testing.T) {
	dir := t.TempDir()
	ex := &laravelExtractor{}

	if ex.Detect(dir) {
		t.Fatal("should not detect without composer.json")
	}

	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(`{"require":{"laravel/framework":"^10.0"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	if !ex.Detect(dir) {
		t.Fatal("should detect laravel/framework in composer.json")
	}
}

func TestLaravelExtractRoutes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(`{"require":{"laravel/framework":"^10.0"}}`), 0644); err != nil {
		t.Fatal(err)
	}

	routesDir := filepath.Join(dir, "routes")
	if err := os.MkdirAll(routesDir, 0755); err != nil {
		t.Fatal(err)
	}

	src := `<?php
use Illuminate\Support\Facades\Route;

Route::get('/users', [UserController::class, 'index']);
Route::post('/users', [UserController::class, 'store']);
Route::get('/users/{user}', [UserController::class, 'show']);
Route::apiResource('posts', PostController::class);
`
	if err := os.WriteFile(filepath.Join(routesDir, "api.php"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &laravelExtractor{}
	endpoints, err := ex.Extract(dir, &config.ScanConfig{})
	if err != nil {
		t.Fatal(err)
	}

	if len(endpoints) < 3 {
		t.Fatalf("expected at least 3 endpoints, got %d", len(endpoints))
	}

	found := map[string]bool{}
	for _, ep := range endpoints {
		found[ep.Method+":"+ep.PathPattern] = true
	}
	for _, want := range []string{"GET:/users", "POST:/users", "GET:/users/{user}"} {
		if !found[want] {
			t.Errorf("missing %s; found: %v", want, found)
		}
	}
}

func TestLaravelRuleToSchema(t *testing.T) {
	tests := []struct {
		rules    string
		wantType string
		wantFmt  string
	}{
		{"required|string|max:255", "string", ""},
		{"required|email", "string", "email"},
		{"required|integer", "integer", ""},
		{"required|boolean", "boolean", ""},
	}
	for _, tt := range tests {
		s := laravelRuleToSchema(tt.rules)
		if s.Type != tt.wantType {
			t.Errorf("rule %q type = %q, want %q", tt.rules, s.Type, tt.wantType)
		}
		if s.Format != tt.wantFmt {
			t.Errorf("rule %q format = %q, want %q", tt.rules, s.Format, tt.wantFmt)
		}
	}
}
