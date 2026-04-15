package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AgusRdz/probe/config"
)

func TestRailsDetect(t *testing.T) {
	dir := t.TempDir()
	ex := &railsExtractor{}

	if ex.Detect(dir) {
		t.Fatal("should not detect without Gemfile")
	}

	if err := os.WriteFile(filepath.Join(dir, "Gemfile"), []byte(`gem "rails", "~> 7.0"`), 0644); err != nil {
		t.Fatal(err)
	}
	if !ex.Detect(dir) {
		t.Fatal("should detect rails in Gemfile")
	}
}

func TestRailsExtractResources(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Gemfile"), []byte(`gem "rails"`), 0644); err != nil {
		t.Fatal(err)
	}

	configDir := filepath.Join(dir, "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}

	routes := `Rails.application.routes.draw do
  resources :users
  resources :posts, only: [:index, :create, :show]
end
`
	if err := os.WriteFile(filepath.Join(configDir, "routes.rb"), []byte(routes), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &railsExtractor{}
	endpoints, err := ex.Extract(dir, &config.ScanConfig{})
	if err != nil {
		t.Fatal(err)
	}

	// resources :users → 6 routes; resources :posts only: 3 → 3 routes
	if len(endpoints) < 9 {
		t.Fatalf("expected at least 9 endpoints, got %d", len(endpoints))
	}
}

func TestRailsExtractNamespace(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Gemfile"), []byte(`gem "rails"`), 0644); err != nil {
		t.Fatal(err)
	}

	configDir := filepath.Join(dir, "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}

	routes := `Rails.application.routes.draw do
  namespace :api do
    get "/health", to: "health#check"
  end
end
`
	if err := os.WriteFile(filepath.Join(configDir, "routes.rb"), []byte(routes), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &railsExtractor{}
	endpoints, err := ex.Extract(dir, &config.ScanConfig{})
	if err != nil {
		t.Fatal(err)
	}

	if len(endpoints) == 0 {
		t.Fatal("expected at least 1 endpoint in namespace")
	}

	found := false
	for _, ep := range endpoints {
		if ep.Method == "GET" && ep.PathPattern == "/api/health" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected GET /api/health; got: %v", endpoints)
	}
}
