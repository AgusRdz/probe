package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AgusRdz/probe/config"
)

func TestGoGinDetect(t *testing.T) {
	dir := t.TempDir()
	ex := &goGinExtractor{}

	if ex.Detect(dir) {
		t.Fatal("should not detect without go.mod")
	}

	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\nrequire github.com/gin-gonic/gin v1.9.0"), 0644); err != nil {
		t.Fatal(err)
	}
	if !ex.Detect(dir) {
		t.Fatal("should detect gin-gonic/gin")
	}
}

func TestGoGinExtractRoutes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\nrequire github.com/gin-gonic/gin v1.9.0"), 0644); err != nil {
		t.Fatal(err)
	}

	src := `package main

import "github.com/gin-gonic/gin"

func main() {
	r := gin.Default()
	r.GET("/ping", ping)
	r.POST("/users", createUser)
	r.GET("/users/:id", getUser)
}

func ping(c *gin.Context) {}
func createUser(c *gin.Context) {}
func getUser(c *gin.Context) {}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &goGinExtractor{}
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
	if !found["GET:/ping"] {
		t.Error("missing GET:/ping")
	}
	if !found["POST:/users"] {
		t.Error("missing POST:/users")
	}
	// :id should be normalized to {id}
	if !found["GET:/users/{id}"] {
		t.Errorf("missing GET:/users/{id}, found: %v", found)
	}
}

func TestGoGinPathParamNormalization(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/users/:id", "/users/{id}"},
		{"/files/*path", "/files/{path}"},
		{"/a/:b/:c", "/a/{b}/{c}"},
	}
	for _, tt := range tests {
		got := normalizeGinPath(tt.input)
		if got != tt.want {
			t.Errorf("normalizeGinPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
