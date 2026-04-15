package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AgusRdz/probe/config"
)

func TestActixDetect(t *testing.T) {
	dir := t.TempDir()
	ex := &actixExtractor{}

	if ex.Detect(dir) {
		t.Fatal("should not detect without Cargo.toml")
	}

	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(`[dependencies]
actix-web = "4"`), 0644); err != nil {
		t.Fatal(err)
	}
	if !ex.Detect(dir) {
		t.Fatal("should detect actix-web in Cargo.toml")
	}
}

func TestActixExtractRoutes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(`[dependencies]
actix-web = "4"`), 0644); err != nil {
		t.Fatal(err)
	}

	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	src := `use actix_web::{get, post, web, HttpResponse};
use serde::Deserialize;

#[derive(Deserialize)]
struct CreateUser {
    name: String,
    email: String,
    age: Option<u32>,
}

#[get("/users")]
async fn list_users() -> HttpResponse {
    HttpResponse::Ok().finish()
}

#[post("/users")]
async fn create_user(body: web::Json<CreateUser>) -> HttpResponse {
    HttpResponse::Created().finish()
}

#[get("/users/{id}")]
async fn get_user(path: web::Path<u32>) -> HttpResponse {
    HttpResponse::Ok().finish()
}
`
	if err := os.WriteFile(filepath.Join(srcDir, "main.rs"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &actixExtractor{}
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
	for _, want := range []string{"GET:/users", "POST:/users", "GET:/users/{id}"} {
		if !found[want] {
			t.Errorf("missing %s; found: %v", want, found)
		}
	}
}

func TestActixNormalizePathParams(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/users/{id}", "/users/{id}"},
		{"/users/{id:\\d+}", "/users/{id}"},
		{"/files/{path:.*}", "/files/{path}"},
	}
	for _, tt := range tests {
		got := normalizeActixPath(tt.input)
		if got != tt.want {
			t.Errorf("normalizeActixPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
