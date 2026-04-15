package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

// TestNormalizeFrameworkPath verifies Express/Rails colon params and Axum angle params
// are converted to {param} format.
func TestNormalizeFrameworkPath(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{":id", "{id}"},
		{"/users/:id/orders", "/users/{id}/orders"},
		{"{id}", "{id}"},
		{"/v1/items", "/v1/items"},
		{"/users/:userId/posts/:postId", "/users/{userId}/posts/{postId}"},
		{"<param>", "{param}"},
		{"/v2/things/<thingId>", "/v2/things/{thingId}"},
		{"/no-params", "/no-params"},
	}
	for _, c := range cases {
		got := NormalizeFrameworkPath(c.input)
		if got != c.want {
			t.Errorf("NormalizeFrameworkPath(%q) = %q; want %q", c.input, got, c.want)
		}
	}
}

// TestDetectFrameworks_Express verifies that a directory containing a package.json
// with "express" in dependencies is detected as "express".
func TestDetectFrameworks_Express(t *testing.T) {
	dir := t.TempDir()
	pkg := `{
  "name": "my-app",
  "dependencies": {
    "express": "^4.18.0"
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0644); err != nil {
		t.Fatal(err)
	}

	frameworks := DetectFrameworks(dir)
	found := false
	for _, f := range frameworks {
		if f == "express" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'express' to be detected, got: %v", frameworks)
	}
}

// TestDetectFrameworks_NoDeps verifies that an empty directory returns no frameworks.
func TestDetectFrameworks_NoDeps(t *testing.T) {
	dir := t.TempDir()
	frameworks := DetectFrameworks(dir)
	if len(frameworks) != 0 {
		t.Errorf("expected no frameworks detected in empty dir, got: %v", frameworks)
	}
}

// TestDetectFrameworks_ASPNET verifies that a directory with a .csproj file
// returns both aspnet-mvc and aspnet-minimal.
func TestDetectFrameworks_ASPNET(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "MyApp.csproj"), []byte(`<Project Sdk="Microsoft.NET.Sdk.Web" />`), 0644); err != nil {
		t.Fatal(err)
	}

	frameworks := DetectFrameworks(dir)
	hasMVC := false
	hasMinimal := false
	for _, f := range frameworks {
		switch f {
		case "aspnet-mvc":
			hasMVC = true
		case "aspnet-minimal":
			hasMinimal = true
		}
	}
	if !hasMVC {
		t.Errorf("expected 'aspnet-mvc' in %v", frameworks)
	}
	if !hasMinimal {
		t.Errorf("expected 'aspnet-minimal' in %v", frameworks)
	}
}
