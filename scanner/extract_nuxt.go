package scanner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AgusRdz/probe/config"
)

// nuxtExtractor implements Extractor for Nuxt.js applications.
type nuxtExtractor struct{}

func (n *nuxtExtractor) Name() string { return "nuxt" }

// Detect returns true if the directory contains a package.json with "nuxt".
func (n *nuxtExtractor) Detect(dir string) bool {
	pkgPath := filepath.Join(dir, "package.json")
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), `"nuxt"`)
}

// allHTTPMethods lists all standard HTTP methods emitted for method-less server files.
var allHTTPMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE"}

// nuxtMethodSuffixes maps filename suffixes (before the final extension) to HTTP methods.
var nuxtMethodSuffixes = map[string]string{
	".get":    "GET",
	".post":   "POST",
	".put":    "PUT",
	".patch":  "PATCH",
	".delete": "DELETE",
}

// Extract walks dir and returns all discovered Nuxt endpoints.
func (n *nuxtExtractor) Extract(dir string, cfg *config.ScanConfig) ([]ScannedEndpoint, error) {
	var endpoints []ScannedEndpoint

	// Pages directory — file-based routing, GET only.
	pagesDir := filepath.Join(dir, "pages")
	if _, err := os.Stat(pagesDir); err == nil {
		found, err := extractNuxtPages(pagesDir, pagesDir)
		if err != nil {
			fmt.Fprintf(errorWriter, "scanner/nuxt: pages error: %v\n", err)
		} else {
			endpoints = append(endpoints, found...)
		}
	}

	// Server API routes — /api prefix.
	serverAPIDir := filepath.Join(dir, "server", "api")
	if _, err := os.Stat(serverAPIDir); err == nil {
		found, err := extractNuxtServerRoutes(serverAPIDir, serverAPIDir, "/api")
		if err != nil {
			fmt.Fprintf(errorWriter, "scanner/nuxt: server/api error: %v\n", err)
		} else {
			endpoints = append(endpoints, found...)
		}
	}

	// Server routes — no /api prefix.
	serverRoutesDir := filepath.Join(dir, "server", "routes")
	if _, err := os.Stat(serverRoutesDir); err == nil {
		found, err := extractNuxtServerRoutes(serverRoutesDir, serverRoutesDir, "")
		if err != nil {
			fmt.Fprintf(errorWriter, "scanner/nuxt: server/routes error: %v\n", err)
		} else {
			endpoints = append(endpoints, found...)
		}
	}

	return endpoints, nil
}

// extractNuxtPages walks the pages directory and produces GET endpoints from file paths.
func extractNuxtPages(baseDir, dir string) ([]ScannedEndpoint, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var endpoints []ScannedEndpoint
	for _, e := range entries {
		name := e.Name()
		fullPath := filepath.Join(dir, name)

		if e.IsDir() {
			found, err := extractNuxtPages(baseDir, fullPath)
			if err != nil {
				fmt.Fprintf(errorWriter, "scanner/nuxt: error in %s: %v\n", fullPath, err)
				continue
			}
			endpoints = append(endpoints, found...)
			continue
		}

		if filepath.Ext(name) != ".vue" {
			continue
		}

		rel, err := filepath.Rel(baseDir, fullPath)
		if err != nil {
			continue
		}

		urlPath := nuxtFileToURLPath(rel)
		absPath, _ := filepath.Abs(fullPath)

		endpoints = append(endpoints, ScannedEndpoint{
			Method:      "GET",
			PathPattern: urlPath,
			Protocol:    "rest",
			Framework:   "nuxt",
			SourceFile:  absPath,
			SourceLine:  1,
		})
	}
	return endpoints, nil
}

// extractNuxtServerRoutes walks server/api or server/routes and produces endpoints.
// prefix is "/api" for server/api or "" for server/routes.
func extractNuxtServerRoutes(baseDir, dir string, prefix string) ([]ScannedEndpoint, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var endpoints []ScannedEndpoint
	for _, e := range entries {
		name := e.Name()
		fullPath := filepath.Join(dir, name)

		if e.IsDir() {
			found, err := extractNuxtServerRoutes(baseDir, fullPath, prefix)
			if err != nil {
				fmt.Fprintf(errorWriter, "scanner/nuxt: error in %s: %v\n", fullPath, err)
				continue
			}
			endpoints = append(endpoints, found...)
			continue
		}

		ext := filepath.Ext(name)
		if ext != ".ts" && ext != ".js" {
			continue
		}

		rel, err := filepath.Rel(baseDir, fullPath)
		if err != nil {
			continue
		}

		methods, urlPath := nuxtServerFileToMethodAndPath(rel, prefix)
		absPath, _ := filepath.Abs(fullPath)

		for _, method := range methods {
			endpoints = append(endpoints, ScannedEndpoint{
				Method:      method,
				PathPattern: urlPath,
				Protocol:    "rest",
				Framework:   "nuxt",
				SourceFile:  absPath,
				SourceLine:  1,
			})
		}
	}
	return endpoints, nil
}

// nuxtFileToURLPath converts a pages/ relative file path to a URL path.
// pages/index.vue → /
// pages/users/index.vue → /users
// pages/users/[id].vue → /users/{id}
// pages/[...slug].vue → /{slug}
func nuxtFileToURLPath(rel string) string {
	rel = filepath.ToSlash(rel)

	// Remove .vue extension.
	rel = strings.TrimSuffix(rel, ".vue")

	parts := strings.Split(rel, "/")
	var normalized []string
	for _, p := range parts {
		if p == "index" {
			continue
		}
		normalized = append(normalized, nuxtSegmentToParam(p))
	}

	if len(normalized) == 0 {
		return "/"
	}
	return "/" + strings.Join(normalized, "/")
}

// nuxtServerFileToMethodAndPath parses a server route filename into HTTP methods and URL path.
// server/api/users.get.ts → ["GET"], /api/users
// server/api/users/[id].ts → allHTTPMethods, /api/users/{id}
func nuxtServerFileToMethodAndPath(rel string, prefix string) ([]string, string) {
	rel = filepath.ToSlash(rel)

	// Remove final extension (.ts or .js).
	for _, ext := range []string{".ts", ".js"} {
		if strings.HasSuffix(rel, ext) {
			rel = rel[:len(rel)-len(ext)]
			break
		}
	}

	// Check for method suffix: .get, .post, .put, .patch, .delete
	var method string
	for suffix, m := range nuxtMethodSuffixes {
		if strings.HasSuffix(rel, suffix) {
			method = m
			rel = rel[:len(rel)-len(suffix)]
			break
		}
	}

	// Build URL path from remaining path segments.
	parts := strings.Split(rel, "/")
	var normalized []string
	for _, p := range parts {
		if p == "index" {
			continue
		}
		normalized = append(normalized, nuxtSegmentToParam(p))
	}

	urlPath := prefix + "/" + strings.Join(normalized, "/")
	// Clean up double slashes.
	urlPath = strings.ReplaceAll(urlPath, "//", "/")
	if urlPath == "" {
		urlPath = "/"
	}

	if method != "" {
		return []string{method}, urlPath
	}
	return allHTTPMethods, urlPath
}

// nuxtSegmentToParam converts Nuxt dynamic and catch-all segments to {param} format.
// [id] → {id}
// [...slug] → {slug}
func nuxtSegmentToParam(segment string) string {
	if strings.HasPrefix(segment, "[...") && strings.HasSuffix(segment, "]") {
		return "{" + segment[4:len(segment)-1] + "}"
	}
	if strings.HasPrefix(segment, "[") && strings.HasSuffix(segment, "]") {
		return "{" + segment[1:len(segment)-1] + "}"
	}
	return segment
}
