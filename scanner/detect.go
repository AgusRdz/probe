package scanner

import (
	"os"
	"path/filepath"
	"strings"
)

// skipDirs are directories never descended into during detection walks.
var skipDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	".git":         true,
	"__pycache__":  true,
}

// DetectFrameworks returns all frameworks detected in dir.
// Checks indicator files (package.json, go.mod, requirements.txt, etc.)
// and inspects their content for known framework dependencies.
// Returns an empty slice if nothing is detected.
func DetectFrameworks(dir string) []string {
	var detected []string
	seen := map[string]bool{}

	add := func(fw string) {
		if !seen[fw] {
			seen[fw] = true
			detected = append(detected, fw)
		}
	}

	// Walk up to 2 levels deep from dir.
	walkDepth(dir, 0, 2, func(path string, d os.DirEntry) {
		if d.IsDir() {
			return
		}
		base := d.Name()
		switch base {
		case "package.json":
			for _, fw := range detectFromPackageJSON(path) {
				add(fw)
			}
		case "go.mod":
			for _, fw := range detectFromGoMod(path) {
				add(fw)
			}
		case "requirements.txt":
			for _, fw := range detectFromRequirementsTxt(path) {
				add(fw)
			}
		case "pyproject.toml":
			for _, fw := range detectFromPyprojectToml(path) {
				add(fw)
			}
		case "pom.xml":
			for _, fw := range detectFromPomXML(path) {
				add(fw)
			}
		case "build.gradle":
			for _, fw := range detectFromBuildGradle(path) {
				add(fw)
			}
		case "build.gradle.kts", "settings.gradle.kts":
			for _, fw := range detectFromGradleKts(path) {
				add(fw)
			}
		case "Gemfile":
			for _, fw := range detectFromGemfile(path) {
				add(fw)
			}
		case "composer.json":
			for _, fw := range detectFromComposerJSON(path) {
				add(fw)
			}
		case "Cargo.toml":
			for _, fw := range detectFromCargoToml(path) {
				add(fw)
			}
		default:
			ext := filepath.Ext(base)
			if ext == ".csproj" || ext == ".sln" {
				add("aspnet-mvc")
				add("aspnet-minimal")
			}
		}
	})

	return detected
}

// walkDepth walks dir up to maxDepth levels deep, calling fn for each entry.
// Skips dirs in skipDirs.
func walkDepth(dir string, depth, maxDepth int, fn func(path string, d os.DirEntry)) {
	if depth > maxDepth {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() && skipDirs[e.Name()] {
			continue
		}
		fullPath := filepath.Join(dir, e.Name())
		fn(fullPath, e)
		if e.IsDir() {
			walkDepth(fullPath, depth+1, maxDepth, fn)
		}
	}
}

func readFileLower(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.ToLower(string(data))
}

func detectFromPackageJSON(path string) []string {
	content := readFileLower(path)
	if content == "" {
		return nil
	}
	var found []string
	// Check for known JS/TS framework deps.
	checks := []struct {
		needle string
		fw     string
	}{
		{`"express"`, "express"},
		{`"fastify"`, "fastify"},
		{`"@nestjs/core"`, "nestjs"},
		{`"next"`, "nextjs"},
		{`"hono"`, "hono"},
		{`"@trpc/server"`, "trpc"},
		{`"koa"`, "koa"},
	}
	for _, c := range checks {
		if strings.Contains(content, c.needle) {
			found = append(found, c.fw)
		}
	}
	return found
}

func detectFromGoMod(path string) []string {
	content := readFileLower(path)
	if content == "" {
		return nil
	}
	type goCheck struct {
		needle string
		fw     string
	}
	checks := []goCheck{
		{"github.com/go-chi/chi", "chi"},
		{"github.com/gin-gonic/gin", "gin"},
		{"github.com/labstack/echo", "echo"},
		{"github.com/gofiber/fiber", "fiber"},
		{"github.com/gorilla/mux", "gorilla"},
	}
	var found []string
	for _, c := range checks {
		if strings.Contains(content, c.needle) {
			found = append(found, c.fw)
		}
	}
	// If it's a go.mod but has no known web framework, treat as go-stdlib.
	if len(found) == 0 && strings.Contains(content, "module ") {
		found = append(found, "go-stdlib")
	}
	return found
}

func detectFromRequirementsTxt(path string) []string {
	content := readFileLower(path)
	if content == "" {
		return nil
	}
	return detectPythonFrameworks(content)
}

func detectFromPyprojectToml(path string) []string {
	content := readFileLower(path)
	if content == "" {
		return nil
	}
	return detectPythonFrameworks(content)
}

func detectPythonFrameworks(content string) []string {
	var found []string
	checks := []struct {
		needle string
		fw     string
	}{
		{"fastapi", "fastapi"},
		{"flask", "flask"},
		{"django", "django"},
		{"litestar", "litestar"},
	}
	for _, c := range checks {
		if strings.Contains(content, c.needle) {
			found = append(found, c.fw)
		}
	}
	return found
}

func detectFromPomXML(path string) []string {
	content := readFileLower(path)
	if content == "" {
		return nil
	}
	var found []string
	if strings.Contains(content, "spring-boot") {
		found = append(found, "spring")
	}
	if strings.Contains(content, "javax.ws.rs") || strings.Contains(content, "jakarta.ws.rs") {
		found = append(found, "jaxrs")
	}
	return found
}

func detectFromBuildGradle(path string) []string {
	content := readFileLower(path)
	if content == "" {
		return nil
	}
	var found []string
	if strings.Contains(content, "spring-boot") {
		found = append(found, "spring")
	}
	if strings.Contains(content, "javax.ws.rs") || strings.Contains(content, "jakarta.ws.rs") {
		found = append(found, "jaxrs")
	}
	return found
}

func detectFromGradleKts(path string) []string {
	content := readFileLower(path)
	if content == "" {
		return nil
	}
	var found []string
	if strings.Contains(content, "ktor") {
		found = append(found, "ktor")
	}
	return found
}

func detectFromGemfile(path string) []string {
	content := readFileLower(path)
	if content == "" {
		return nil
	}
	var found []string
	if strings.Contains(content, "rails") {
		found = append(found, "rails")
	}
	if strings.Contains(content, "sinatra") {
		found = append(found, "sinatra")
	}
	return found
}

func detectFromComposerJSON(path string) []string {
	content := readFileLower(path)
	if content == "" {
		return nil
	}
	var found []string
	if strings.Contains(content, "laravel/framework") {
		found = append(found, "laravel")
	}
	if strings.Contains(content, "symfony/framework-bundle") {
		found = append(found, "symfony")
	}
	return found
}

func detectFromCargoToml(path string) []string {
	content := readFileLower(path)
	if content == "" {
		return nil
	}
	var found []string
	if strings.Contains(content, "actix-web") {
		found = append(found, "actix")
	}
	if strings.Contains(content, "axum") {
		found = append(found, "axum")
	}
	return found
}
