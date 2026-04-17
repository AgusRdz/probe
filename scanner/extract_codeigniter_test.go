package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AgusRdz/probe/config"
)

func TestCodeIgniterDetect(t *testing.T) {
	dir := t.TempDir()
	ex := &codeigniterExtractor{}

	if ex.Detect(dir) {
		t.Fatal("should not detect without composer.json")
	}

	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(`{"require":{"codeigniter4/framework":"^4.0"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	if !ex.Detect(dir) {
		t.Fatal("should detect codeigniter4/framework in composer.json")
	}
}

func TestCodeIgniterBasicVerbRoutes(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "app", "Config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(`{"require":{"codeigniter4/framework":"^4.0"}}`), 0644); err != nil {
		t.Fatal(err)
	}

	src := `<?php
$routes->get('/users', 'Users::index');
$routes->post('/users', 'Users::create');
$routes->put('/users/(:num)', 'Users::update/$1');
$routes->delete('/users/(:num)', 'Users::delete/$1');
`
	if err := os.WriteFile(filepath.Join(configDir, "Routes.php"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &codeigniterExtractor{}
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

func TestCodeIgniterResourceExpansion(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "app", "Config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(`{"require":{"codeigniter4/framework":"^4.0"}}`), 0644); err != nil {
		t.Fatal(err)
	}

	src := `<?php
$routes->resource('photos');
`
	if err := os.WriteFile(filepath.Join(configDir, "Routes.php"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &codeigniterExtractor{}
	endpoints, err := ex.Extract(dir, &config.ScanConfig{})
	if err != nil {
		t.Fatal(err)
	}

	found := map[string]bool{}
	for _, ep := range endpoints {
		found[ep.Method+":"+ep.PathPattern] = true
	}

	for _, want := range []string{
		"GET:/photos",
		"GET:/photos/{id}",
		"POST:/photos",
		"PUT:/photos/{id}",
		"PATCH:/photos/{id}",
		"DELETE:/photos/{id}",
	} {
		if !found[want] {
			t.Errorf("missing resource route %s; found: %v", want, found)
		}
	}
}

func TestCodeIgniterGroupPrefix(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "app", "Config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(`{"require":{"codeigniter4/framework":"^4.0"}}`), 0644); err != nil {
		t.Fatal(err)
	}

	src := `<?php
$routes->group('api', function($routes) {
    $routes->get('users', 'Users::index');
});
`
	if err := os.WriteFile(filepath.Join(configDir, "Routes.php"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &codeigniterExtractor{}
	endpoints, err := ex.Extract(dir, &config.ScanConfig{})
	if err != nil {
		t.Fatal(err)
	}

	found := map[string]bool{}
	for _, ep := range endpoints {
		found[ep.Method+":"+ep.PathPattern] = true
	}

	if !found["GET:/api/users"] {
		t.Errorf("missing GET:/api/users; found: %v", found)
	}
}

func TestCodeIgniterPathParamConversion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/users/(:num)", "/users/{id}"},
		{"/posts/(:alpha)", "/posts/{param}"},
		{"/items/(:segment)/detail", "/items/{param}/detail"},
		{"/files/(:any)", "/files/{param}"},
		{"/already/{id}", "/already/{id}"},
	}
	for _, tt := range tests {
		got := normalizeCodeIgniterPath(tt.input)
		if got != tt.want {
			t.Errorf("normalizeCodeIgniterPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
