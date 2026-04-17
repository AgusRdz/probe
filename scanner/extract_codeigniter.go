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

// codeigniterExtractor implements Extractor for CodeIgniter 4 PHP applications.
type codeigniterExtractor struct{}

func (e *codeigniterExtractor) Name() string { return "codeigniter" }

// Detect returns true if composer.json mentions codeigniter4/framework or codeigniter/framework.
func (e *codeigniterExtractor) Detect(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "composer.json"))
	if err != nil {
		return false
	}
	lower := strings.ToLower(string(data))
	return strings.Contains(lower, "codeigniter4/framework") ||
		strings.Contains(lower, "codeigniter/framework")
}

// $routes->get('/path', 'Handler::method')
var reCI4Route = regexp.MustCompile(
	`\$routes->(get|post|put|patch|delete|options)\s*\(\s*['"]([^'"]+)['"]`,
)

// $routes->resource('photos') or $routes->presenter('photos')
var reCI4Resource = regexp.MustCompile(
	`\$routes->(resource|presenter)\s*\(\s*['"]([^'"]+)['"]`,
)

// $routes->group('prefix', function($routes) {
var reCI4Group = regexp.MustCompile(
	`\$routes->group\s*\(\s*['"]([^'"]+)['"]`,
)

// Extract walks app/Config/Routes.php (and fallback to any Routes.php).
func (e *codeigniterExtractor) Extract(dir string, cfg *config.ScanConfig) ([]ScannedEndpoint, error) {
	var endpoints []ScannedEndpoint

	routesFile := filepath.Join(dir, "app", "Config", "Routes.php")
	if _, err := os.Stat(routesFile); err == nil {
		found, err := extractCI4RouteFile(routesFile)
		if err != nil {
			fmt.Fprintf(errorWriter, "scanner/codeigniter: error reading %s: %v\n", routesFile, err)
		} else {
			endpoints = append(endpoints, found...)
		}
		return endpoints, nil
	}

	// Fallback: walk for Routes.php anywhere in the project.
	err := walkWithSkip(dir, map[string]bool{
		"vendor":       true,
		"node_modules": true,
		".git":         true,
	}, ".php", func(path string) error {
		if filepath.Base(path) != "Routes.php" {
			return nil
		}
		found, ferr := extractCI4RouteFile(path)
		if ferr != nil {
			fmt.Fprintf(errorWriter, "scanner/codeigniter: error reading %s: %v\n", path, ferr)
			return nil
		}
		endpoints = append(endpoints, found...)
		return nil
	})
	return endpoints, err
}

// extractCI4RouteFile parses a CodeIgniter 4 Routes.php file.
func extractCI4RouteFile(path string) ([]ScannedEndpoint, error) {
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

	// Track group prefix stack. Simple single-level detection per line.
	// For nested groups this is a best-effort scan.
	prefix := ""
	var endpoints []ScannedEndpoint

	for i, line := range lines {
		// Detect group start.
		if m := reCI4Group.FindStringSubmatch(line); m != nil {
			prefix = "/" + strings.Trim(m[1], "/")
			continue
		}
		// Detect group end (closing brace on its own).
		if strings.TrimSpace(line) == "});" || strings.TrimSpace(line) == "}" {
			prefix = ""
			continue
		}

		// Single verb route.
		if m := reCI4Route.FindStringSubmatch(line); m != nil {
			method := strings.ToUpper(m[1])
			rawPath := m[2]
			fullPath := normalizeCodeIgniterPath(prefix + "/" + strings.TrimLeft(rawPath, "/"))
			endpoints = append(endpoints, ScannedEndpoint{
				Method:      method,
				PathPattern: fullPath,
				Protocol:    "rest",
				Framework:   "codeigniter",
				SourceFile:  absPath,
				SourceLine:  i + 1,
			})
			continue
		}

		// Resource / presenter expansion.
		if m := reCI4Resource.FindStringSubmatch(line); m != nil {
			name := strings.TrimLeft(m[2], "/")
			base := normalizeCodeIgniterPath(prefix + "/" + name)
			resourceRoutes := []struct {
				method string
				suffix string
			}{
				{"GET", ""},
				{"GET", "/{id}"},
				{"POST", ""},
				{"PUT", "/{id}"},
				{"PATCH", "/{id}"},
				{"DELETE", "/{id}"},
			}
			for _, rr := range resourceRoutes {
				endpoints = append(endpoints, ScannedEndpoint{
					Method:      rr.method,
					PathPattern: base + rr.suffix,
					Protocol:    "rest",
					Framework:   "codeigniter",
					SourceFile:  absPath,
					SourceLine:  i + 1,
				})
			}
		}
	}

	return endpoints, nil
}

// normalizeCodeIgniterPath converts CI4 path syntax to probe's {param} format
// and cleans double slashes.
func normalizeCodeIgniterPath(path string) string {
	// Convert CI4 wildcards.
	path = strings.ReplaceAll(path, "(:num)", "{id}")
	path = strings.ReplaceAll(path, "(:alpha)", "{param}")
	path = strings.ReplaceAll(path, "(:segment)", "{param}")
	path = strings.ReplaceAll(path, "(:any)", "{param}")

	// Collapse double slashes.
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}
