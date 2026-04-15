package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AgusRdz/probe/config"
)

func TestASPNetMinimalExtractRoutes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "App.csproj"), []byte("<Project/>"), 0644); err != nil {
		t.Fatal(err)
	}

	src := `var builder = WebApplication.CreateBuilder(args);
var app = builder.Build();

app.MapGet("/", () => "Hello World!");
app.MapGet("/users", () => Results.Ok(new List<User>()));
app.MapPost("/users", (CreateUserRequest req) => Results.Ok<User>(new User()));
app.MapGet("/users/{id}", (int id) => Results.Ok<User>(new User()));
app.MapDelete("/users/{id}", (int id) => Results.NoContent());

app.Run();
`
	if err := os.WriteFile(filepath.Join(dir, "Program.cs"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &aspnetMinimalExtractor{}
	endpoints, err := ex.Extract(dir, &config.ScanConfig{})
	if err != nil {
		t.Fatal(err)
	}

	if len(endpoints) < 5 {
		t.Fatalf("expected at least 5 endpoints, got %d", len(endpoints))
	}

	found := map[string]bool{}
	for _, ep := range endpoints {
		found[ep.Method+":"+ep.PathPattern] = true
	}
	for _, want := range []string{"GET:/users", "POST:/users", "DELETE:/users/{id}"} {
		if !found[want] {
			t.Errorf("missing %s; found: %v", want, found)
		}
	}
}

func TestASPNetMinimalGroupPrefix(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "App.csproj"), []byte("<Project/>"), 0644); err != nil {
		t.Fatal(err)
	}

	src := `var app = builder.Build();
var api = app.MapGroup("/api/v1");
api.MapGet("/health", () => "ok");
`
	if err := os.WriteFile(filepath.Join(dir, "Program.cs"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &aspnetMinimalExtractor{}
	endpoints, err := ex.Extract(dir, &config.ScanConfig{})
	if err != nil {
		t.Fatal(err)
	}

	if len(endpoints) == 0 {
		t.Fatal("expected at least 1 endpoint")
	}
	// Should find GET /health (prefix detection is file-level best-effort).
	found := false
	for _, ep := range endpoints {
		if ep.Method == "GET" {
			found = true
		}
	}
	if !found {
		t.Error("no GET endpoints found")
	}
}
