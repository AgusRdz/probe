package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AgusRdz/probe/config"
)

func TestZendDetect(t *testing.T) {
	dir := t.TempDir()
	ex := &zendExtractor{}

	if ex.Detect(dir) {
		t.Fatal("should not detect without composer.json")
	}

	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(`{"require":{"laminas/laminas-mvc":"^3.0"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	if !ex.Detect(dir) {
		t.Fatal("should detect laminas/laminas-mvc in composer.json")
	}
}

func TestZendLiteralRoute(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(`{"require":{"laminas/laminas-mvc":"^3.0"}}`), 0644); err != nil {
		t.Fatal(err)
	}

	src := `<?php
return [
    'router' => [
        'routes' => [
            'users' => [
                'type' => Literal::class,
                'options' => [
                    'route' => '/users',
                    'defaults' => ['action' => 'index'],
                ],
            ],
        ],
    ],
];
`
	if err := os.WriteFile(filepath.Join(dir, "module.config.php"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &zendExtractor{}
	endpoints, err := ex.Extract(dir, &config.ScanConfig{})
	if err != nil {
		t.Fatal(err)
	}

	found := map[string]bool{}
	for _, ep := range endpoints {
		found[ep.Method+":"+ep.PathPattern] = true
	}

	if !found["GET:/users"] {
		t.Errorf("missing GET:/users; found: %v", found)
	}
}

func TestZendSegmentRouteOptionalParam(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(`{"require":{"laminas/laminas-mvc":"^3.0"}}`), 0644); err != nil {
		t.Fatal(err)
	}

	src := `<?php
return [
    'router' => [
        'routes' => [
            'user' => [
                'type' => Segment::class,
                'options' => [
                    'route' => '/users[/:id]',
                ],
            ],
        ],
    ],
];
`
	if err := os.WriteFile(filepath.Join(dir, "module.config.php"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &zendExtractor{}
	endpoints, err := ex.Extract(dir, &config.ScanConfig{})
	if err != nil {
		t.Fatal(err)
	}

	found := map[string]bool{}
	for _, ep := range endpoints {
		found[ep.Method+":"+ep.PathPattern] = true
	}

	if !found["GET:/users/{id}"] {
		t.Errorf("missing GET:/users/{id}; found: %v", found)
	}
}

func TestZendMultipleRoutes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(`{"require":{"laminas/laminas-mvc":"^3.0"}}`), 0644); err != nil {
		t.Fatal(err)
	}

	src := `<?php
return [
    'router' => [
        'routes' => [
            'home' => [
                'type' => Literal::class,
                'options' => ['route' => '/'],
            ],
            'users' => [
                'type' => Literal::class,
                'options' => ['route' => '/users'],
            ],
            'user' => [
                'type' => Segment::class,
                'options' => ['route' => '/users[/:id]'],
            ],
            'posts' => [
                'type' => Segment::class,
                'options' => ['route' => '/posts/:slug'],
            ],
        ],
    ],
];
`
	if err := os.WriteFile(filepath.Join(dir, "module.config.php"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &zendExtractor{}
	endpoints, err := ex.Extract(dir, &config.ScanConfig{})
	if err != nil {
		t.Fatal(err)
	}

	found := map[string]bool{}
	for _, ep := range endpoints {
		found[ep.Method+":"+ep.PathPattern] = true
	}

	for _, want := range []string{
		"GET:/",
		"GET:/users",
		"GET:/users/{id}",
		"GET:/posts/{slug}",
	} {
		if !found[want] {
			t.Errorf("missing %s; found: %v", want, found)
		}
	}
}

func TestZendNormalizeZendPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/users[/:id]", "/users/{id}"},
		{"/users/:id", "/users/{id}"},
		{"/posts/:slug/comments", "/posts/{slug}/comments"},
		{"/simple", "/simple"},
		{"/api/v1/items[/:id]", "/api/v1/items/{id}"},
	}
	for _, tt := range tests {
		got := normalizeZendPath(tt.input)
		if got != tt.want {
			t.Errorf("normalizeZendPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
