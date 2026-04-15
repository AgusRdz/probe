package scanner

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/AgusRdz/probe/config"
	"github.com/AgusRdz/probe/observer"
)

// flaskExtractor implements Extractor for Flask applications.
type flaskExtractor struct{}

func (e *flaskExtractor) Name() string { return "flask" }

// Detect returns true if requirements.txt or pyproject.toml mentions flask but not fastapi.
func (e *flaskExtractor) Detect(dir string) bool {
	if pyProjectContains(dir, "fastapi") {
		return false
	}
	return pyProjectContains(dir, "flask")
}

// Flask route decorator: @app.route("/path", methods=["GET", "POST"])
var reFlaskRoute = regexp.MustCompile(
	`(?i)@(?:app|blueprint|\w+)\.route\s*\(\s*["']([^"']+)["']`,
)

// Flask methods= extraction: methods=["GET", "POST"]
var reFlaskMethods = regexp.MustCompile(`methods\s*=\s*\[([^\]]+)\]`)

// Flask Blueprint prefix: Blueprint("name", url_prefix="/prefix")
var reFlaskBlueprintPrefix = regexp.MustCompile(
	`Blueprint\s*\([^)]*url_prefix\s*=\s*["']([^"']+)["']`,
)

// flask-pydantic @validate(body=Schema)
var reFlaskPydanticValidate = regexp.MustCompile(`@validate\s*\([^)]*body\s*=\s*(\w+)`)

// Extract walks dir and returns all discovered Flask endpoints.
func (e *flaskExtractor) Extract(dir string, cfg *config.ScanConfig) ([]ScannedEndpoint, error) {
	// Collect Pydantic models in case flask-pydantic is used.
	models := make(map[string]*observer.Schema)
	_ = walkPythonFiles(dir, func(path string) error {
		found, ferr := extractFastAPIModels(path)
		if ferr != nil {
			return nil
		}
		for k, v := range found {
			models[k] = v
		}
		return nil
	})

	var endpoints []ScannedEndpoint
	err := walkPythonFiles(dir, func(path string) error {
		found, ferr := extractFlaskFile(path, models)
		if ferr != nil {
			fmt.Fprintf(errorWriter, "scanner/flask: error reading %s: %v\n", path, ferr)
			return nil
		}
		endpoints = append(endpoints, found...)
		return nil
	})
	return endpoints, err
}

// extractFlaskFile parses a single .py file for Flask routes.
func extractFlaskFile(path string, models map[string]*observer.Schema) ([]ScannedEndpoint, error) {
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

	// Detect blueprint prefix.
	prefix := ""
	for _, line := range lines {
		if m := reFlaskBlueprintPrefix.FindStringSubmatch(line); m != nil {
			prefix = m[1]
			break
		}
	}

	var endpoints []ScannedEndpoint
	for i, line := range lines {
		m := reFlaskRoute.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		rawPath := m[1]
		fullPath := NormalizeFrameworkPath(prefix + rawPath)

		// Determine HTTP methods.
		methods := []string{"GET"} // Flask default.
		if mm := reFlaskMethods.FindStringSubmatch(line); mm != nil {
			methods = parseFlaskMethods(mm[1])
		}

		// Check next few lines for @validate(body=...).
		var reqSchema *observer.Schema
		end := i + 5
		if end > len(lines) {
			end = len(lines)
		}
		for j := i + 1; j < end; j++ {
			if vm := reFlaskPydanticValidate.FindStringSubmatch(lines[j]); vm != nil {
				if s, ok := models[vm[1]]; ok {
					reqSchema = s
				}
			}
		}

		for _, method := range methods {
			ep := ScannedEndpoint{
				Method:      method,
				PathPattern: fullPath,
				Protocol:    "rest",
				Framework:   "flask",
				SourceFile:  absPath,
				SourceLine:  i + 1,
				ReqSchema:   reqSchema,
			}
			endpoints = append(endpoints, ep)
		}
	}
	return endpoints, nil
}

// parseFlaskMethods parses Flask methods=["GET", "POST"] interior.
func parseFlaskMethods(interior string) []string {
	var methods []string
	re := regexp.MustCompile(`["']([A-Z]+)["']`)
	for _, m := range re.FindAllStringSubmatch(interior, -1) {
		methods = append(methods, strings.ToUpper(m[1]))
	}
	if len(methods) == 0 {
		return []string{"GET"}
	}
	return methods
}
