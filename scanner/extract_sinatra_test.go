package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AgusRdz/probe/config"
)

func TestSinatraDetect(t *testing.T) {
	dir := t.TempDir()
	ex := &sinatraExtractor{}

	if ex.Detect(dir) {
		t.Fatal("should not detect without Gemfile")
	}

	if err := os.WriteFile(filepath.Join(dir, "Gemfile"), []byte(`gem "sinatra", "~> 3.0"`), 0644); err != nil {
		t.Fatal(err)
	}
	if !ex.Detect(dir) {
		t.Fatal("should detect sinatra in Gemfile")
	}
}

func TestSinatraExtractBasicRoutes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Gemfile"), []byte(`gem "sinatra"`), 0644); err != nil {
		t.Fatal(err)
	}

	src := `require 'sinatra'

get '/users' do
  # handler
end

post '/users' do
  # handler
end

put '/users/:id' do
  # handler
end

delete '/users/:id' do
  # handler
end
`
	if err := os.WriteFile(filepath.Join(dir, "app.rb"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &sinatraExtractor{}
	endpoints, err := ex.Extract(dir, &config.ScanConfig{})
	if err != nil {
		t.Fatal(err)
	}

	found := map[string]bool{}
	for _, ep := range endpoints {
		found[ep.Method+":"+ep.PathPattern] = true
	}

	for _, want := range []string{
		"GET:/users",
		"POST:/users",
		"PUT:/users/{id}",
		"DELETE:/users/{id}",
	} {
		if !found[want] {
			t.Errorf("missing %s; found: %v", want, found)
		}
	}
}

func TestSinatraExtractPathParams(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Gemfile"), []byte(`gem "sinatra"`), 0644); err != nil {
		t.Fatal(err)
	}

	src := `get '/posts/:post_id/comments/:id' do
end
`
	if err := os.WriteFile(filepath.Join(dir, "app.rb"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &sinatraExtractor{}
	endpoints, err := ex.Extract(dir, &config.ScanConfig{})
	if err != nil {
		t.Fatal(err)
	}

	if len(endpoints) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(endpoints))
	}
	if endpoints[0].PathPattern != "/posts/{post_id}/comments/{id}" {
		t.Errorf("unexpected path pattern: %q", endpoints[0].PathPattern)
	}
}

func TestSinatraExtractDeprecated(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Gemfile"), []byte(`gem "sinatra"`), 0644); err != nil {
		t.Fatal(err)
	}

	src := `# @deprecated use /v2/users instead
get '/users' do
end

get '/v2/users' do
end
`
	if err := os.WriteFile(filepath.Join(dir, "app.rb"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &sinatraExtractor{}
	endpoints, err := ex.Extract(dir, &config.ScanConfig{})
	if err != nil {
		t.Fatal(err)
	}

	if len(endpoints) < 2 {
		t.Fatalf("expected 2 endpoints, got %d", len(endpoints))
	}

	deprecatedCount := 0
	for _, ep := range endpoints {
		if ep.PathPattern == "/users" && ep.Deprecated {
			deprecatedCount++
		}
		if ep.PathPattern == "/v2/users" && ep.Deprecated {
			t.Errorf("/v2/users should not be deprecated")
		}
	}
	if deprecatedCount != 1 {
		t.Errorf("expected 1 deprecated endpoint, got %d", deprecatedCount)
	}
}
