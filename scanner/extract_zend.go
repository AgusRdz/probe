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

// zendExtractor implements Extractor for Zend Framework / Laminas MVC applications.
type zendExtractor struct{}

func (e *zendExtractor) Name() string { return "zend" }

// Detect returns true if composer.json mentions laminas-mvc, zend-mvc, or laminas-router.
func (e *zendExtractor) Detect(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "composer.json"))
	if err != nil {
		return false
	}
	lower := strings.ToLower(string(data))
	return strings.Contains(lower, "laminas/laminas-mvc") ||
		strings.Contains(lower, "zendframework/zend-mvc") ||
		strings.Contains(lower, "laminas/laminas-router")
}

// 'route' => '/some/path' (single-quoted)
var reZendRouteSingle = regexp.MustCompile(`'route'\s*=>\s*'([^']+)'`)

// "route" => "/some/path" (double-quoted)
var reZendRouteDouble = regexp.MustCompile(`"route"\s*=>\s*"([^"]+)"`)

// Extract walks module.config.php and config/autoload/*.php files.
func (e *zendExtractor) Extract(dir string, cfg *config.ScanConfig) ([]ScannedEndpoint, error) {
	var endpoints []ScannedEndpoint

	// Priority: module config files.
	_ = walkWithSkip(dir, map[string]bool{
		"vendor":       true,
		"node_modules": true,
		".git":         true,
	}, ".php", func(path string) error {
		base := filepath.Base(path)
		// Only process files likely to contain route config.
		if base != "module.config.php" && !isZendAutoloadConfig(path) {
			return nil
		}
		found, ferr := extractZendConfigFile(path)
		if ferr != nil {
			fmt.Fprintf(errorWriter, "scanner/zend: error reading %s: %v\n", path, ferr)
			return nil
		}
		endpoints = append(endpoints, found...)
		return nil
	})

	return endpoints, nil
}

// isZendAutoloadConfig returns true if the path is under config/autoload/.
func isZendAutoloadConfig(path string) bool {
	normalized := filepath.ToSlash(path)
	return strings.Contains(normalized, "config/autoload/")
}

// extractZendConfigFile parses a Zend/Laminas PHP config file for route entries.
func extractZendConfigFile(path string) ([]ScannedEndpoint, error) {
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
	var endpoints []ScannedEndpoint

	for i, line := range lines {
		var rawRoute string

		if m := reZendRouteSingle.FindStringSubmatch(line); m != nil {
			rawRoute = m[1]
		} else if m := reZendRouteDouble.FindStringSubmatch(line); m != nil {
			rawRoute = m[1]
		} else {
			continue
		}

		// Skip non-path values (e.g. class names, keywords).
		if !strings.HasPrefix(rawRoute, "/") {
			continue
		}

		fullPath := normalizeZendPath(rawRoute)
		endpoints = append(endpoints, ScannedEndpoint{
			Method:      "GET",
			PathPattern: fullPath,
			Protocol:    "rest",
			Framework:   "zend",
			SourceFile:  absPath,
			SourceLine:  i + 1,
		})
	}

	return endpoints, nil
}

// normalizeZendPath converts Zend/Laminas route syntax to probe's {param} format.
// Handles [/:id] (optional segment) and /:id (required segment).
func normalizeZendPath(path string) string {
	// Remove optional brackets: [/:param] → /{param}
	reOptional := regexp.MustCompile(`\[/:([A-Za-z_][A-Za-z0-9_]*)\]`)
	path = reOptional.ReplaceAllString(path, `/{$1}`)

	// Convert required :param → {param}
	reRequired := regexp.MustCompile(`/:([A-Za-z_][A-Za-z0-9_]*)`)
	path = reRequired.ReplaceAllString(path, `/{$1}`)

	// Collapse double slashes.
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}
