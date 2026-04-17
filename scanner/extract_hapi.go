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

// hapiExtractor implements Extractor for Hapi.js applications.
type hapiExtractor struct{}

func (h *hapiExtractor) Name() string { return "hapi" }

// Detect returns true if the directory contains a package.json with "@hapi/hapi" or "hapi".
func (h *hapiExtractor) Detect(dir string) bool {
	pkgPath := filepath.Join(dir, "package.json")
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return false
	}
	lower := strings.ToLower(string(data))
	return strings.Contains(lower, `"@hapi/hapi"`) || strings.Contains(lower, `"hapi"`)
}

// reHapiRoute matches the start of a Hapi route registration.
// Matches: server.route({ or anything.route({
var reHapiRoute = regexp.MustCompile(`\.route\s*\(\s*\{`)

// reHapiMethod matches: method: 'GET' or method: "GET"
var reHapiMethod = regexp.MustCompile(`method\s*:\s*'([A-Z]+)'|method\s*:\s*"([A-Z]+)"`)

// reHapiMethodArray matches: method: ['GET', 'POST'] or method: ["GET", "POST"]
var reHapiMethodArray = regexp.MustCompile(`method\s*:\s*\[([^\]]+)\]`)

// reHapiMethodItem matches individual method strings inside an array.
var reHapiMethodItem = regexp.MustCompile(`['"]([A-Z]+)['"]`)

// reHapiPath matches: path: '/users' or path: "/users"
var reHapiPath = regexp.MustCompile(`path\s*:\s*'([^']+)'|path\s*:\s*"([^"]+)"`)

// skipHapiDirs are sub-directories to skip during Hapi scanning.
var skipHapiDirs = map[string]bool{
	"node_modules": true,
	"dist":         true,
	".next":        true,
	"coverage":     true,
	"vendor":       true,
	".git":         true,
	"__pycache__":  true,
}

// Extract walks dir and returns all discovered Hapi endpoints.
func (h *hapiExtractor) Extract(dir string, cfg *config.ScanConfig) ([]ScannedEndpoint, error) {
	var endpoints []ScannedEndpoint
	err := walkJS(dir, skipHapiDirs, func(path string) error {
		found, err := extractHapiFile(path)
		if err != nil {
			fmt.Fprintf(errorWriter, "scanner/hapi: error reading %s: %v\n", path, err)
			return nil
		}
		endpoints = append(endpoints, found...)
		return nil
	})
	return endpoints, err
}

// extractHapiFile parses a single JS/TS file for Hapi route definitions.
func extractHapiFile(path string) ([]ScannedEndpoint, error) {
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
	absPath, _ := filepath.Abs(path)

	var endpoints []ScannedEndpoint

	for i, line := range lines {
		if !reHapiRoute.MatchString(line) {
			continue
		}

		// Collect up to 10 lines starting from this line to find method and path.
		end := i + 10
		if end > len(lines) {
			end = len(lines)
		}
		block := strings.Join(lines[i:end], "\n")

		methods := extractHapiMethods(block)
		rawPath := extractHapiPath(block)
		if rawPath == "" || len(methods) == 0 {
			continue
		}

		normalizedPath := NormalizeFrameworkPath(rawPath)

		var info jsdocInfo
		if block, ok := jsdocBlocks[i]; ok {
			info = block
		}

		for _, method := range methods {
			ep := ScannedEndpoint{
				Method:      method,
				PathPattern: normalizedPath,
				Protocol:    "rest",
				Framework:   "hapi",
				SourceFile:  absPath,
				SourceLine:  i + 1,
				Description: info.description,
				Deprecated:  info.deprecated,
			}
			endpoints = append(endpoints, ep)
		}
	}

	return endpoints, nil
}

// extractHapiMethods extracts HTTP method(s) from a Hapi route block.
func extractHapiMethods(block string) []string {
	// Check for array form first: method: ['GET', 'POST']
	if am := reHapiMethodArray.FindStringSubmatch(block); am != nil {
		items := reHapiMethodItem.FindAllStringSubmatch(am[1], -1)
		var methods []string
		seen := make(map[string]bool)
		for _, item := range items {
			m := strings.ToUpper(item[1])
			if !seen[m] {
				seen[m] = true
				methods = append(methods, m)
			}
		}
		if len(methods) > 0 {
			return methods
		}
	}

	// Single string form: method: 'GET'
	if sm := reHapiMethod.FindStringSubmatch(block); sm != nil {
		m := sm[1]
		if m == "" {
			m = sm[2]
		}
		if m != "" {
			return []string{strings.ToUpper(m)}
		}
	}

	return nil
}

// extractHapiPath extracts the path value from a Hapi route block.
func extractHapiPath(block string) string {
	if pm := reHapiPath.FindStringSubmatch(block); pm != nil {
		if pm[1] != "" {
			return pm[1]
		}
		return pm[2]
	}
	return ""
}
