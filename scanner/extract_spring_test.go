package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AgusRdz/probe/config"
)

func TestSpringDetect(t *testing.T) {
	dir := t.TempDir()
	ex := &springExtractor{}

	if ex.Detect(dir) {
		t.Fatal("should not detect without pom.xml")
	}

	if err := os.WriteFile(filepath.Join(dir, "pom.xml"), []byte("<spring-boot-starter-web>"), 0644); err != nil {
		t.Fatal(err)
	}
	if !ex.Detect(dir) {
		t.Fatal("should detect with spring-boot in pom.xml")
	}
}

func TestSpringExtractRoutes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pom.xml"), []byte("spring-boot"), 0644); err != nil {
		t.Fatal(err)
	}

	src := `package com.example.app;

import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/api/users")
public class UserController {

    /** Get all users */
    @GetMapping
    public ResponseEntity<List<User>> getAll() {
        return ResponseEntity.ok(null);
    }

    @GetMapping("/{id}")
    public ResponseEntity<User> getById(@PathVariable Long id) {
        return ResponseEntity.ok(null);
    }

    @PostMapping
    public ResponseEntity<User> create(@RequestBody CreateUserRequest request) {
        return ResponseEntity.ok(null);
    }

    @Deprecated
    @DeleteMapping("/{id}")
    public ResponseEntity<Void> delete(@PathVariable Long id) {
        return ResponseEntity.noContent().build();
    }
}
`
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "UserController.java"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &springExtractor{}
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
		if ep.Method == "DELETE" && !ep.Deprecated {
			t.Error("DELETE endpoint should be marked deprecated")
		}
	}
}

func TestSpringJavaTypeToSchema(t *testing.T) {
	tests := []struct {
		javaType string
		wantType string
	}{
		{"String", "string"},
		{"Integer", "integer"},
		{"Long", "integer"},
		{"Boolean", "boolean"},
		{"Double", "number"},
		{"List<String>", "array"},
	}
	for _, tt := range tests {
		s := javaTypeToSchema(tt.javaType)
		if s.Type != tt.wantType {
			t.Errorf("javaTypeToSchema(%q).Type = %q, want %q", tt.javaType, s.Type, tt.wantType)
		}
	}
}
