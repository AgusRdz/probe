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

// laravelExtractor implements Extractor for Laravel PHP applications.
type laravelExtractor struct{}

func (e *laravelExtractor) Name() string { return "laravel" }

// Detect returns true if composer.json mentions laravel/framework.
func (e *laravelExtractor) Detect(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "composer.json"))
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), "laravel/framework")
}

// Route::get('/path', handler) or Route::post('/path', handler)
var reLaravelRoute = regexp.MustCompile(
	`Route::(get|post|put|patch|delete|options|any)\s*\(\s*['"]([^'"]+)['"]`,
)

// Route::apiResource('users', Controller::class)
var reLaravelAPIResource = regexp.MustCompile(
	`Route::apiResource\s*\(\s*['"]([^'"]+)['"]`,
)

// Route::resource('users', Controller::class) (web resource — same methods)
var reLaravelResource = regexp.MustCompile(
	`Route::resource\s*\(\s*['"]([^'"]+)['"]`,
)

// Route::group(['prefix' => 'v1'], ...)
var reLaravelGroupPrefix = regexp.MustCompile(
	`Route::group\s*\(\s*\[.*?['"]prefix['"]\s*=>\s*['"]([^'"]+)['"]`,
)

// Route::prefix('api')->group(...)
var reLaravelPrefixChain = regexp.MustCompile(
	`Route::prefix\s*\(\s*['"]([^'"]+)['"]`,
)

// FormRequest rules: 'name' => 'required|string|max:255'
var reLaravelRule = regexp.MustCompile(
	`['"](\w+)['"]\s*=>\s*['"]([^'"]+)['"]`,
)

// public function rules(): array
var reLaravelRulesMethod = regexp.MustCompile(`function rules\s*\(\s*\)`)

// {param} is already correct in Laravel routes.

// Extract walks routes/ and FormRequest files.
func (e *laravelExtractor) Extract(dir string, cfg *config.ScanConfig) ([]ScannedEndpoint, error) {
	// Collect FormRequest schemas.
	schemas := make(map[string]*observer.Schema)
	requestsDir := filepath.Join(dir, "app", "Http", "Requests")
	_ = walkWithSkip(requestsDir, map[string]bool{}, ".php", func(path string) error {
		found, ferr := extractLaravelFormRequest(path)
		if ferr != nil {
			return nil
		}
		for k, v := range found {
			schemas[k] = v
		}
		return nil
	})

	var endpoints []ScannedEndpoint
	routesDir := filepath.Join(dir, "routes")
	err := walkWithSkip(routesDir, map[string]bool{}, ".php", func(path string) error {
		found, ferr := extractLaravelRouteFile(path, schemas)
		if ferr != nil {
			fmt.Fprintf(errorWriter, "scanner/laravel: error reading %s: %v\n", path, ferr)
			return nil
		}
		endpoints = append(endpoints, found...)
		return nil
	})
	if err != nil {
		// routes dir may not exist — try walking all .php.
		err = walkWithSkip(dir, map[string]bool{
			"vendor":       true,
			"node_modules": true,
			".git":         true,
			"storage":      true,
		}, ".php", func(path string) error {
			base := filepath.Base(path)
			if base != "api.php" && base != "web.php" {
				return nil
			}
			found, ferr := extractLaravelRouteFile(path, schemas)
			if ferr != nil {
				fmt.Fprintf(errorWriter, "scanner/laravel: error reading %s: %v\n", path, ferr)
				return nil
			}
			endpoints = append(endpoints, found...)
			return nil
		})
	}
	return endpoints, err
}

// extractLaravelRouteFile parses a Laravel routes file.
func extractLaravelRouteFile(path string, schemas map[string]*observer.Schema) ([]ScannedEndpoint, error) {
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

	// Detect file-level prefix.
	prefix := ""
	for _, line := range lines {
		if m := reLaravelGroupPrefix.FindStringSubmatch(line); m != nil {
			prefix = m[1]
			break
		}
		if m := reLaravelPrefixChain.FindStringSubmatch(line); m != nil {
			prefix = m[1]
			break
		}
	}

	var endpoints []ScannedEndpoint
	for i, line := range lines {
		// Single verb route.
		if m := reLaravelRoute.FindStringSubmatch(line); m != nil {
			method := strings.ToUpper(m[1])
			rawPath := m[2]
			fullPath := normalizeFrameworkPathLaravel(prefix + "/" + strings.TrimLeft(rawPath, "/"))
			endpoints = append(endpoints, ScannedEndpoint{
				Method:      method,
				PathPattern: fullPath,
				Protocol:    "rest",
				Framework:   "laravel",
				SourceFile:  absPath,
				SourceLine:  i + 1,
			})
			continue
		}

		// apiResource or resource — generates standard CRUD routes.
		for _, re := range []*regexp.Regexp{reLaravelAPIResource, reLaravelResource} {
			if m := re.FindStringSubmatch(line); m != nil {
				name := m[1]
				base := normalizeFrameworkPathLaravel(prefix + "/" + strings.TrimLeft(name, "/"))
				paramName := strings.TrimSuffix(strings.TrimRight(name, "s"), "/")
				param := "{" + paramName + "}"
				crudRoutes := []struct {
					method string
					suffix string
				}{
					{"GET", ""},
					{"POST", ""},
					{"GET", "/" + param},
					{"PUT", "/" + param},
					{"DELETE", "/" + param},
				}
				for _, cr := range crudRoutes {
					endpoints = append(endpoints, ScannedEndpoint{
						Method:      cr.method,
						PathPattern: base + cr.suffix,
						Protocol:    "rest",
						Framework:   "laravel",
						SourceFile:  absPath,
						SourceLine:  i + 1,
					})
				}
				break
			}
		}
	}
	return endpoints, nil
}

// extractLaravelFormRequest parses a FormRequest class for validation rules.
func extractLaravelFormRequest(path string) (map[string]*observer.Schema, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Class name from filename.
	base := strings.TrimSuffix(filepath.Base(path), ".php")
	result := make(map[string]*observer.Schema)

	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	inRules := false
	schema := &observer.Schema{
		Type:       "object",
		Properties: make(map[string]*observer.Schema),
	}

	for _, line := range lines {
		if reLaravelRulesMethod.MatchString(line) {
			inRules = true
			continue
		}
		if inRules {
			if strings.TrimSpace(line) == "}" {
				inRules = false
				break
			}
			if m := reLaravelRule.FindStringSubmatch(line); m != nil {
				fieldName := m[1]
				rules := m[2]
				s := laravelRuleToSchema(rules)
				schema.Properties[fieldName] = &s
				if strings.Contains(rules, "required") {
					schema.Required = append(schema.Required, fieldName)
				}
			}
		}
	}

	if len(schema.Properties) > 0 {
		result[base] = schema
	}
	return result, nil
}

// laravelRuleToSchema converts a Laravel validation rule string to observer.Schema.
func laravelRuleToSchema(rules string) observer.Schema {
	s := observer.Schema{Type: "string"}
	for _, rule := range strings.Split(rules, "|") {
		rule = strings.TrimSpace(rule)
		switch {
		case rule == "integer" || rule == "numeric":
			s.Type = "integer"
		case rule == "boolean":
			s.Type = "boolean"
		case rule == "array":
			s.Type = "array"
		case rule == "email":
			s.Format = "email"
		case strings.HasPrefix(rule, "max:"):
			s.MaxLength = atoi(strings.TrimPrefix(rule, "max:"))
		case strings.HasPrefix(rule, "min:"):
			s.MinLength = atoi(strings.TrimPrefix(rule, "min:"))
		}
	}
	return s
}

// normalizeFrameworkPathLaravel cleans double slashes and ensures leading slash.
func normalizeFrameworkPathLaravel(path string) string {
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}
