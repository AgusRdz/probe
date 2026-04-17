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

// symfonyExtractor implements Extractor for Symfony PHP applications.
type symfonyExtractor struct{}

func (e *symfonyExtractor) Name() string { return "symfony" }

// Detect returns true if composer.json mentions symfony/framework-bundle.
func (e *symfonyExtractor) Detect(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "composer.json"))
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), "symfony/framework-bundle")
}

// PHP8 attribute: #[Route('/path', ...)] or #[Route('/path', name: '...', methods: ['GET'])]
var reSymfonyAttr = regexp.MustCompile(`#\[Route\(\s*['"]([^'"]+)['"]([^)]*)\)`)

// methods: ['GET', 'POST'] inside attribute arguments
var reSymfonyAttrMethods = regexp.MustCompile(`methods\s*:\s*\[([^\]]+)\]`)

// Legacy annotation: * @Route("/path", methods={"GET","POST"})
var reSymfonyAnnotation = regexp.MustCompile(`\*\s*@Route\(\s*["']([^"']+)["']([^)]*)\)`)

// methods={"GET","POST"} inside annotation arguments
var reSymfonyAnnotationMethods = regexp.MustCompile(`methods\s*=\s*\{([^}]+)\}`)

// class keyword — used to detect class-level prefix boundary
var reSymfonyClass = regexp.MustCompile(`\bclass\s+\w+`)

// @deprecated PHPDoc or @Deprecated annotation
var reSymfonyDeprecated = regexp.MustCompile(`(@[Dd]eprecated|@deprecated)`)

// Extract walks src/ and app/ directories for PHP controller files.
func (e *symfonyExtractor) Extract(dir string, cfg *config.ScanConfig) ([]ScannedEndpoint, error) {
	var endpoints []ScannedEndpoint
	skip := map[string]bool{
		"vendor":       true,
		"node_modules": true,
		".git":         true,
		"var":          true,
		"cache":        true,
	}

	for _, subdir := range []string{"src", "app"} {
		target := filepath.Join(dir, subdir)
		if _, err := os.Stat(target); err != nil {
			continue
		}
		walkErr := walkWithSkip(target, skip, ".php", func(path string) error {
			found, ferr := extractSymfonyFile(path)
			if ferr != nil {
				fmt.Fprintf(errorWriter, "scanner/symfony: error reading %s: %v\n", path, ferr)
				return nil
			}
			endpoints = append(endpoints, found...)
			return nil
		})
		if walkErr != nil {
			fmt.Fprintf(errorWriter, "scanner/symfony: error walking %s: %v\n", target, walkErr)
		}
	}
	return endpoints, nil
}

// extractSymfonyFile parses a single PHP file for Symfony route attributes/annotations.
func extractSymfonyFile(path string) ([]ScannedEndpoint, error) {
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

	absPath, _ := filepath.Abs(path)

	// First pass: detect class-level Route prefix (appears before the class keyword).
	classPrefix := ""
	for i, line := range lines {
		if reSymfonyClass.MatchString(line) {
			// Look back for a #[Route(...)] or @Route(...) without methods — that's the class prefix.
			for j := i - 1; j >= 0 && j >= i-5; j-- {
				prev := lines[j]
				if m := reSymfonyAttr.FindStringSubmatch(prev); m != nil {
					// Class-level attribute: only use it if no methods specified.
					if !reSymfonyAttrMethods.MatchString(m[2]) {
						classPrefix = m[1]
					}
				} else if m := reSymfonyAnnotation.FindStringSubmatch(prev); m != nil {
					if !reSymfonyAnnotationMethods.MatchString(m[2]) {
						classPrefix = m[1]
					}
				}
			}
			break
		}
	}

	var endpoints []ScannedEndpoint

	for i, line := range lines {
		// Skip class-level route lines (already used as prefix above).
		isClassLevel := false
		for j := i + 1; j < len(lines) && j <= i+5; j++ {
			if reSymfonyClass.MatchString(lines[j]) {
				isClassLevel = true
				break
			}
			// Stop looking forward if we hit another route or non-blank/comment line.
			trimmed := strings.TrimSpace(lines[j])
			if trimmed != "" && !strings.HasPrefix(trimmed, "#[") && !strings.HasPrefix(trimmed, "//") &&
				!strings.HasPrefix(trimmed, "*") && !strings.HasPrefix(trimmed, "/*") {
				break
			}
		}
		if isClassLevel {
			continue
		}

		var routePath string
		var methodsStr string
		matched := false

		if m := reSymfonyAttr.FindStringSubmatch(line); m != nil {
			routePath = m[1]
			methodsStr = m[2]
			matched = true
		} else if m := reSymfonyAnnotation.FindStringSubmatch(line); m != nil {
			routePath = m[1]
			methodsStr = m[2]
			matched = true
		}

		if !matched {
			continue
		}

		fullPath := normalizeSymfonyPath(classPrefix + "/" + strings.TrimLeft(routePath, "/"))
		methods := parseSymfonyMethods(methodsStr)

		deprecated := false
		// Check preceding lines for @deprecated.
		for j := i - 1; j >= 0 && j >= i-10; j-- {
			prev := lines[j]
			if reSymfonyDeprecated.MatchString(prev) {
				deprecated = true
				break
			}
			trimmed := strings.TrimSpace(prev)
			// Stop at blank lines or end of doc block.
			if trimmed == "" || trimmed == "*/" {
				break
			}
		}

		for _, method := range methods {
			endpoints = append(endpoints, ScannedEndpoint{
				Method:      method,
				PathPattern: fullPath,
				Protocol:    "rest",
				Framework:   "symfony",
				SourceFile:  absPath,
				SourceLine:  i + 1,
				Deprecated:  deprecated,
			})
		}
	}

	return endpoints, nil
}

// parseSymfonyMethods extracts HTTP methods from the route attribute/annotation arguments string.
// Returns ["GET"] as default when no methods are specified.
func parseSymfonyMethods(args string) []string {
	// Try PHP8 attribute style: methods: ['GET', 'POST']
	if m := reSymfonyAttrMethods.FindStringSubmatch(args); m != nil {
		return splitSymfonyMethods(m[1])
	}
	// Try annotation style: methods={"GET","POST"}
	if m := reSymfonyAnnotationMethods.FindStringSubmatch(args); m != nil {
		return splitSymfonyMethods(m[1])
	}
	return []string{"GET"}
}

// splitSymfonyMethods splits a comma-separated list of quoted HTTP method names.
func splitSymfonyMethods(raw string) []string {
	var methods []string
	for _, part := range strings.Split(raw, ",") {
		method := strings.Trim(strings.TrimSpace(part), `'"[]`)
		method = strings.ToUpper(method)
		if method != "" {
			methods = append(methods, method)
		}
	}
	if len(methods) == 0 {
		return []string{"GET"}
	}
	return methods
}

// normalizeSymfonyPath cleans double slashes and ensures a leading slash.
// Symfony routes already use {param} format — no conversion needed.
func normalizeSymfonyPath(path string) string {
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}
