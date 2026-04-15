package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AgusRdz/probe/config"
)

func TestKtorDetect(t *testing.T) {
	dir := t.TempDir()
	ex := &ktorExtractor{}

	if ex.Detect(dir) {
		t.Fatal("should not detect without build.gradle")
	}

	if err := os.WriteFile(filepath.Join(dir, "build.gradle.kts"), []byte(`dependencies {
    implementation("io.ktor:ktor-server-core:2.3.0")
}`), 0644); err != nil {
		t.Fatal(err)
	}
	if !ex.Detect(dir) {
		t.Fatal("should detect ktor in build.gradle.kts")
	}
}

func TestKtorExtractRoutes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "build.gradle.kts"), []byte(`implementation("io.ktor:ktor-server-core")`), 0644); err != nil {
		t.Fatal(err)
	}

	srcDir := filepath.Join(dir, "src", "main", "kotlin")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	src := `package com.example

import io.ktor.server.application.*
import io.ktor.server.routing.*

data class CreateUserRequest(
    val name: String,
    val email: String,
    val age: Int? = null
)

fun Application.configureRouting() {
    routing {
        get("/health") {
            call.respondText("OK")
        }
        route("/users") {
            get("/") {
                call.respond(listOf<User>())
            }
            post("/") {
                val request = call.receive<CreateUserRequest>()
                call.respond(request)
            }
        }
        get("/users/{id}") {
            call.respond("user")
        }
    }
}
`
	if err := os.WriteFile(filepath.Join(srcDir, "Routing.kt"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &ktorExtractor{}
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
	if !found["GET:/health"] {
		t.Errorf("missing GET:/health; found: %v", found)
	}
}

func TestKtorDataClassSchema(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "build.gradle"), []byte("ktor"), 0644); err != nil {
		t.Fatal(err)
	}

	src := `data class UserDto(
    val id: Long,
    val name: String,
    val email: String,
    val age: Int? = null
)
`
	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "User.kt"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	schemas, err := extractKotlinDataClasses(filepath.Join(srcDir, "User.kt"))
	if err != nil {
		t.Fatal(err)
	}

	s, ok := schemas["UserDto"]
	if !ok {
		t.Fatal("expected UserDto schema")
	}
	if s.Properties["name"] == nil {
		t.Error("expected 'name' property")
	}
	if s.Properties["age"] == nil {
		t.Error("expected 'age' property")
	}
	// age is optional — should not be in required
	for _, req := range s.Required {
		if req == "age" {
			t.Error("'age' should not be required (has default)")
		}
	}
}

func TestKtorNormalizePath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/users/{id}", "/users/{id}"},
		{"/users/{id?}", "/users/{id}"},
		{"users", "/users"},
	}
	for _, tt := range tests {
		got := normalizeKtorPath(tt.input)
		if got != tt.want {
			t.Errorf("normalizeKtorPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
