package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AgusRdz/probe/config"
	"github.com/AgusRdz/probe/observer"
)

func TestAxumDetect(t *testing.T) {
	dir := t.TempDir()
	ex := &axumExtractor{}

	if ex.Detect(dir) {
		t.Fatal("should not detect without Cargo.toml")
	}

	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(`[dependencies]
axum = "0.7"`), 0644); err != nil {
		t.Fatal(err)
	}
	if !ex.Detect(dir) {
		t.Fatal("should detect axum in Cargo.toml")
	}
}

func TestAxumExtractRoutes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(`[dependencies]
axum = "0.7"`), 0644); err != nil {
		t.Fatal(err)
	}

	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	src := `use axum::{routing::{get, post}, Router, Json};
use serde::Deserialize;

#[derive(Deserialize)]
struct CreateUser {
    name: String,
    email: String,
}

async fn list_users() -> &'static str { "[]" }

async fn create_user(Json(body): Json<CreateUser>) -> &'static str { "" }

pub fn router() -> Router {
    Router::new()
        .route("/users", get(list_users))
        .route("/users", post(create_user))
        .route("/users/:id", get(list_users))
}
`
	if err := os.WriteFile(filepath.Join(srcDir, "main.rs"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &axumExtractor{}
	endpoints, err := ex.Extract(dir, &config.ScanConfig{})
	if err != nil {
		t.Fatal(err)
	}

	if len(endpoints) < 2 {
		t.Fatalf("expected at least 2 endpoints, got %d", len(endpoints))
	}

	found := map[string]bool{}
	for _, ep := range endpoints {
		found[ep.Method+":"+ep.PathPattern] = true
	}
	if !found["GET:/users"] {
		t.Errorf("missing GET:/users; found: %v", found)
	}
	if !found["POST:/users"] {
		t.Errorf("missing POST:/users; found: %v", found)
	}
}

func TestAxumRustStructSchema(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(`[dependencies]
axum = "0.7"`), 0644); err != nil {
		t.Fatal(err)
	}

	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	src := `use serde::Deserialize;

#[derive(Deserialize)]
struct Payload {
    name: String,
    count: u32,
    label: Option<String>,
}
`
	if err := os.WriteFile(filepath.Join(srcDir, "types.rs"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	schemas := make(map[string]*observer.Schema)
	_ = walkRustFiles(dir, func(path string) error {
		found, _ := extractRustStructSchemas(path)
		for k, v := range found {
			schemas[k] = v
		}
		return nil
	})

	if _, ok := schemas["Payload"]; !ok {
		t.Fatal("expected Payload struct schema to be extracted")
	}
}
