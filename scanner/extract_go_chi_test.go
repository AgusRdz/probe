package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AgusRdz/probe/config"
)

func TestGoChiDetect(t *testing.T) {
	dir := t.TempDir()
	ex := &goChiExtractor{}

	if ex.Detect(dir) {
		t.Fatal("should not detect without go.mod")
	}

	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\nrequire github.com/go-chi/chi v5.0.0"), 0644); err != nil {
		t.Fatal(err)
	}
	if !ex.Detect(dir) {
		t.Fatal("should detect go-chi/chi in go.mod")
	}
}

func TestGoChiExtractRoutes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\nrequire github.com/go-chi/chi v5.0.0"), 0644); err != nil {
		t.Fatal(err)
	}

	src := `package main

import (
	"net/http"
	"github.com/go-chi/chi/v5"
)

func main() {
	r := chi.NewRouter()
	r.Get("/users", listUsers)
	r.Post("/users", createUser)
	r.Get("/users/{userID}", getUser)
	r.Delete("/users/{userID}", deleteUser)
}

func listUsers(w http.ResponseWriter, r *http.Request) {}
func createUser(w http.ResponseWriter, r *http.Request) {}
func getUser(w http.ResponseWriter, r *http.Request) {}
func deleteUser(w http.ResponseWriter, r *http.Request) {}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &goChiExtractor{}
	endpoints, err := ex.Extract(dir, &config.ScanConfig{})
	if err != nil {
		t.Fatal(err)
	}

	if len(endpoints) < 4 {
		t.Fatalf("expected at least 4 endpoints, got %d", len(endpoints))
	}

	found := map[string]bool{}
	for _, ep := range endpoints {
		found[ep.Method+":"+ep.PathPattern] = true
	}
	for _, want := range []string{"GET:/users", "POST:/users", "GET:/users/{userID}", "DELETE:/users/{userID}"} {
		if !found[want] {
			t.Errorf("missing endpoint %s", want)
		}
	}
}

func TestGoChiRoutePrefix(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\nrequire github.com/go-chi/chi v5.0.0"), 0644); err != nil {
		t.Fatal(err)
	}

	src := `package main

import "github.com/go-chi/chi/v5"

func main() {
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		r.Get("/health", healthHandler)
	})
}

func healthHandler(w http.ResponseWriter, r *http.Request) {}
`
	if err := os.WriteFile(filepath.Join(dir, "routes.go"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &goChiExtractor{}
	endpoints, err := ex.Extract(dir, &config.ScanConfig{})
	if err != nil {
		t.Fatal(err)
	}

	if len(endpoints) == 0 {
		t.Fatal("expected at least 1 endpoint")
	}
}
