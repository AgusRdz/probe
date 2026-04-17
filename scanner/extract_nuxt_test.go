package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AgusRdz/probe/config"
)

func TestNuxtExtractor_PagesDynamic(t *testing.T) {
	dir := t.TempDir()

	pageDir := filepath.Join(dir, "pages", "users")
	if err := os.MkdirAll(pageDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pageDir, "[id].vue"), []byte("<template/>"), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &nuxtExtractor{}
	cfg := &config.ScanConfig{}
	endpoints, err := ex.Extract(dir, cfg)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	found := false
	for _, ep := range endpoints {
		if ep.Method == "GET" && ep.PathPattern == "/users/{id}" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected GET /users/{id}, got: %+v", endpoints)
	}
}

func TestNuxtExtractor_PagesIndex(t *testing.T) {
	dir := t.TempDir()

	pageDir := filepath.Join(dir, "pages")
	if err := os.MkdirAll(pageDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pageDir, "index.vue"), []byte("<template/>"), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &nuxtExtractor{}
	cfg := &config.ScanConfig{}
	endpoints, err := ex.Extract(dir, cfg)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	found := false
	for _, ep := range endpoints {
		if ep.Method == "GET" && ep.PathPattern == "/" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected GET /, got: %+v", endpoints)
	}
}

func TestNuxtExtractor_ServerAPIWithMethod(t *testing.T) {
	dir := t.TempDir()

	apiDir := filepath.Join(dir, "server", "api")
	if err := os.MkdirAll(apiDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(apiDir, "users.get.ts"), []byte("export default defineEventHandler(() => {})"), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &nuxtExtractor{}
	cfg := &config.ScanConfig{}
	endpoints, err := ex.Extract(dir, cfg)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	found := false
	for _, ep := range endpoints {
		if ep.Method == "GET" && ep.PathPattern == "/api/users" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected GET /api/users, got: %+v", endpoints)
	}
}

func TestNuxtExtractor_ServerAPIAllMethods(t *testing.T) {
	dir := t.TempDir()

	apiDir := filepath.Join(dir, "server", "api", "users")
	if err := os.MkdirAll(apiDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(apiDir, "[id].ts"), []byte("export default defineEventHandler(() => {})"), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &nuxtExtractor{}
	cfg := &config.ScanConfig{}
	endpoints, err := ex.Extract(dir, cfg)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	wantMethods := []string{"GET", "POST", "PUT", "PATCH", "DELETE"}
	found := make(map[string]bool)
	for _, ep := range endpoints {
		if ep.PathPattern == "/api/users/{id}" {
			found[ep.Method] = true
		}
	}

	for _, m := range wantMethods {
		if !found[m] {
			t.Errorf("expected %s /api/users/{id}, got: %v", m, found)
		}
	}
}

func TestNuxtExtractor_ServerRoutesNoPrefix(t *testing.T) {
	dir := t.TempDir()

	routesDir := filepath.Join(dir, "server", "routes")
	if err := os.MkdirAll(routesDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(routesDir, "health.get.ts"), []byte("export default defineEventHandler(() => 'ok')"), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &nuxtExtractor{}
	cfg := &config.ScanConfig{}
	endpoints, err := ex.Extract(dir, cfg)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	found := false
	for _, ep := range endpoints {
		if ep.Method == "GET" && ep.PathPattern == "/health" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected GET /health, got: %+v", endpoints)
	}
}
