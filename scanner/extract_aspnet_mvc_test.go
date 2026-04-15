package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AgusRdz/probe/config"
)

func TestASPNetMVCDetect(t *testing.T) {
	dir := t.TempDir()
	ex := &aspnetMVCExtractor{}

	if ex.Detect(dir) {
		t.Fatal("should not detect without .csproj")
	}

	if err := os.WriteFile(filepath.Join(dir, "MyApp.csproj"), []byte("<Project/>"), 0644); err != nil {
		t.Fatal(err)
	}
	if !ex.Detect(dir) {
		t.Fatal("should detect with .csproj")
	}
}

func TestASPNetMVCExtractRoutes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "App.csproj"), []byte("<Project/>"), 0644); err != nil {
		t.Fatal(err)
	}

	src := `using Microsoft.AspNetCore.Mvc;

namespace MyApp.Controllers;

[ApiController]
[Route("api/[controller]")]
public class UsersController : ControllerBase
{
    /// <summary>Get all users</summary>
    [HttpGet]
    public ActionResult<IEnumerable<User>> GetAll()
    {
        return Ok();
    }

    [HttpGet("{id}")]
    public ActionResult<User> GetById(int id)
    {
        return Ok();
    }

    [HttpPost]
    public ActionResult Create([FromBody] CreateUserRequest request)
    {
        return Created();
    }
}
`
	if err := os.WriteFile(filepath.Join(dir, "UsersController.cs"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &aspnetMVCExtractor{}
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
	for _, want := range []string{"GET:api/users", "POST:api/users"} {
		hasMatch := false
		for k := range found {
			if k == want || k == "/"+want {
				hasMatch = true
			}
		}
		if !hasMatch {
			t.Logf("all endpoints: %v", found)
			t.Errorf("missing endpoint %s", want)
		}
	}
}

func TestASPNetMVCCSTypeToSchema(t *testing.T) {
	tests := []struct {
		csType string
		want   string
	}{
		{"string", "string"},
		{"int", "integer"},
		{"int?", "integer"},
		{"bool", "boolean"},
		{"double", "number"},
		{"List<string>", "array"},
	}
	for _, tt := range tests {
		s := csTypeToSchema(tt.csType)
		if s.Type != tt.want {
			t.Errorf("csTypeToSchema(%q).Type = %q, want %q", tt.csType, s.Type, tt.want)
		}
	}
}
