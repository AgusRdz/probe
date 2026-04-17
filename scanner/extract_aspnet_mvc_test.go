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

// TestASPNetMVCFrameworkPatterns covers .NET Framework 4.x patterns:
//   - [RoutePrefix] on class instead of [Route]
//   - IHttpActionResult return type instead of IActionResult
//   - Flexible attribute order ([Route] before or after [HttpMethod])
func TestASPNetMVCFrameworkPatterns(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "App.csproj"), []byte("<Project/>"), 0644); err != nil {
		t.Fatal(err)
	}

	src := `using System.Web.Http;

namespace CiraNet.Attorney.Api.Controllers;

[Authorize]
[RoutePrefix("api/account")]
public class AccountController : APBaseApiController
{
    /// <summary>Get account notes</summary>
    [HttpGet]
    [Route("notes")]
    public async Task<IHttpActionResult> GetReferredAccountNotes()
    {
        return Ok();
    }

    [HttpPost]
    [Route("statement")]
    public async Task<IHttpActionResult> PostGenerateStatement([FromBody] StatementRequest request)
    {
        return Ok();
    }

    [AllowAnonymous]
    [Route("resetpassword")]
    [HttpPut]
    public async Task<IHttpActionResult> ResetPassword([FromBody] ResetPasswordRequest request)
    {
        return Ok();
    }

    [HttpGet]
    [Route("{id}")]
    public async Task<IHttpActionResult> GetById(int id)
    {
        return Ok();
    }
}
`
	if err := os.WriteFile(filepath.Join(dir, "AccountController.cs"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &aspnetMVCExtractor{}
	endpoints, err := ex.Extract(dir, &config.ScanConfig{})
	if err != nil {
		t.Fatal(err)
	}

	found := map[string]bool{}
	for _, ep := range endpoints {
		found[ep.Method+":"+ep.PathPattern] = true
	}

	wants := []string{
		"GET:api/account/notes",
		"POST:api/account/statement",
		"PUT:api/account/resetpassword",
		"GET:api/account/{id}",
	}
	for _, want := range wants {
		if !found[want] {
			t.Logf("all endpoints: %v", found)
			t.Errorf("missing endpoint %s", want)
		}
	}
}

// TestASPNetMVCAuthDetection verifies that [Authorize] and [AllowAnonymous]
// attributes are correctly reflected in the RequiresAuth field.
func TestASPNetMVCAuthDetection(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "App.csproj"), []byte("<Project/>"), 0644); err != nil {
		t.Fatal(err)
	}

	src := `using Microsoft.AspNetCore.Mvc;

namespace MyApp.Controllers;

[ApiController]
[Authorize]
[Route("api/auth")]
public class AuthController : ControllerBase
{
    [AllowAnonymous]
    [HttpPost("login")]
    public ActionResult Login([FromBody] LoginRequest request)
    {
        return Ok();
    }

    [HttpGet("profile")]
    public ActionResult<UserProfile> GetProfile()
    {
        return Ok();
    }
}
`
	if err := os.WriteFile(filepath.Join(dir, "AuthController.cs"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &aspnetMVCExtractor{}
	endpoints, err := ex.Extract(dir, &config.ScanConfig{})
	if err != nil {
		t.Fatal(err)
	}

	byPath := map[string]ScannedEndpoint{}
	for _, ep := range endpoints {
		byPath[ep.Method+":"+ep.PathPattern] = ep
	}

	login, ok := byPath["POST:api/auth/login"]
	if !ok {
		t.Fatalf("missing POST:api/auth/login; got %v", byPath)
	}
	if login.RequiresAuth {
		t.Errorf("POST:api/auth/login should have RequiresAuth=false (AllowAnonymous)")
	}

	profile, ok := byPath["GET:api/auth/profile"]
	if !ok {
		t.Fatalf("missing GET:api/auth/profile; got %v", byPath)
	}
	if !profile.RequiresAuth {
		t.Errorf("GET:api/auth/profile should have RequiresAuth=true (inherits class [Authorize])")
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
