package scanner

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/AgusRdz/probe/config"
)

// koaExtractor implements Extractor for Koa applications (using koa-router).
type koaExtractor struct{}

func (e *koaExtractor) Name() string { return "koa" }

// Detect returns true if the directory contains a package.json with koa as a dependency.
func (e *koaExtractor) Detect(dir string) bool {
	pkgPath := filepath.Join(dir, "package.json")
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), `"koa"`)
}

// Koa router method: router.get('/path', handler), app.get('/path', handler), etc.
var reKoaRoute = regexp.MustCompile(
	`(?i)\b(?:router|app|\w+)\.(get|post|put|patch|delete|head|options|all)\s*\(\s*['"]([^'"]+)['"]`,
)

// Router prefix from: new Router({ prefix: '/api' }) or new Router({ prefix: "/api" })
var reKoaRouterPrefix = regexp.MustCompile(
	`new\s+Router\s*\(\s*\{[^}]*prefix\s*:\s*['"]([^'"]+)['"]`,
)

// skipKoaDirs are sub-directories to skip during Koa scanning.
var skipKoaDirs = map[string]bool{
	"node_modules": true,
	"dist":         true,
	".next":        true,
	"coverage":     true,
	"vendor":       true,
	".git":         true,
}

// Extract walks dir and returns all discovered Koa endpoints.
func (e *koaExtractor) Extract(dir string, cfg *config.ScanConfig) ([]ScannedEndpoint, error) {
	var endpoints []ScannedEndpoint
	err := walkJS(dir, skipKoaDirs, func(path string) error {
		found, err := extractKoaFile(path)
		if err != nil {
			fmt.Fprintf(errorWriter, "scanner/koa: error reading %s: %v\n", path, err)
			return nil
		}
		endpoints = append(endpoints, found...)
		return nil
	})
	return endpoints, err
}

// extractKoaFile parses a single JS/TS file for Koa routes.
func extractKoaFile(path string) ([]ScannedEndpoint, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	jsdocBlocks := parseJSDocBlocks(lines)

	// Collect router prefixes defined in this file.
	prefix := extractKoaPrefix(lines)

	var endpoints []ScannedEndpoint
	absPath, _ := filepath.Abs(path)

	for i, line := range lines {
		lineNo := i + 1

		m := reKoaRoute.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		method := strings.ToUpper(m[1])
		rawPath := prefix + m[2]

		ep := ScannedEndpoint{
			Method:      method,
			PathPattern: NormalizeFrameworkPath(rawPath),
			Protocol:    "rest",
			Framework:   "koa",
			SourceFile:  absPath,
			SourceLine:  lineNo,
		}

		if block, ok := jsdocBlocks[i]; ok {
			ep.Description = block.description
			ep.Deprecated = block.deprecated
		}

		endpoints = append(endpoints, ep)
	}

	return endpoints, nil
}

// extractKoaPrefix scans all lines in the file and returns the first router prefix found.
func extractKoaPrefix(lines []string) string {
	for _, line := range lines {
		if pm := reKoaRouterPrefix.FindStringSubmatch(line); pm != nil {
			return pm[1]
		}
	}
	return ""
}
