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

// djangoExtractor implements Extractor for Django applications.
type djangoExtractor struct{}

func (e *djangoExtractor) Name() string { return "django" }

// Detect returns true if requirements.txt or pyproject.toml mentions django.
func (e *djangoExtractor) Detect(dir string) bool {
	return pyProjectContains(dir, "django")
}

// Django urls.py patterns:
//
//	path("users/", UserListView.as_view())
//	path("users/<int:pk>/", UserDetailView.as_view())
var reDjangoPath = regexp.MustCompile(
	`path\s*\(\s*["']([^"']+)["']\s*,\s*(\w+)\.as_view`,
)

// Django router.register("users", UserViewSet) — DRF router
var reDjangoRouter = regexp.MustCompile(
	`router\.register\s*\(\s*["']([^"']+)["']\s*,\s*(\w+)`,
)

// Django URL path param: <int:pk>, <str:slug>, <uuid:id>
var reDjangoPathParam = regexp.MustCompile(`<(?:\w+:)?(\w+)>`)

// DRF serializer class: class UserSerializer(ModelSerializer):
var reDjangoSerializer = regexp.MustCompile(`^class\s+(\w+Serializer)\s*\(`)

// DRF serializer fields = [...] (on one line)
var reDjangoSerializerFields = regexp.MustCompile(`fields\s*=\s*\[([^\]]+)\]`)

// Django include() for nested urls.
var reDjangoInclude = regexp.MustCompile(`include\s*\(\s*["']([^"']+)["']`)

// DRF ViewSet CRUD methods exposed by router.register.
var djangoCRUDMethods = []struct {
	method string
	suffix string
}{
	{"GET", ""},
	{"POST", ""},
	{"GET", "/{id}"},
	{"PUT", "/{id}"},
	{"PATCH", "/{id}"},
	{"DELETE", "/{id}"},
}

// Extract walks the project and returns all discovered Django endpoints.
func (e *djangoExtractor) Extract(dir string, cfg *config.ScanConfig) ([]ScannedEndpoint, error) {
	// Collect serializer schemas.
	serializers := make(map[string]*observer.Schema)
	_ = walkPythonFiles(dir, func(path string) error {
		if !strings.HasSuffix(path, "serializers.py") &&
			!strings.Contains(path, "serializer") {
			return nil
		}
		found, ferr := extractDjangoSerializers(path)
		if ferr != nil {
			return nil
		}
		for k, v := range found {
			serializers[k] = v
		}
		return nil
	})

	var endpoints []ScannedEndpoint
	err := walkPythonFiles(dir, func(path string) error {
		base := filepath.Base(path)
		if base != "urls.py" &&
			!strings.HasSuffix(base, "views.py") &&
			!strings.Contains(base, "views") {
			return nil
		}
		found, ferr := extractDjangoFile(path, serializers)
		if ferr != nil {
			fmt.Fprintf(errorWriter, "scanner/django: error reading %s: %v\n", path, ferr)
			return nil
		}
		endpoints = append(endpoints, found...)
		return nil
	})
	return endpoints, err
}

// extractDjangoSerializers extracts DRF serializer schemas from a file.
func extractDjangoSerializers(path string) (map[string]*observer.Schema, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	result := make(map[string]*observer.Schema)
	var currentName string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if m := reDjangoSerializer.FindStringSubmatch(line); m != nil {
			currentName = m[1]
			continue
		}
		if currentName != "" {
			if m := reDjangoSerializerFields.FindStringSubmatch(line); m != nil {
				schema := &observer.Schema{
					Type:       "object",
					Properties: make(map[string]*observer.Schema),
				}
				for _, rawField := range strings.Split(m[1], ",") {
					fieldName := strings.Trim(strings.TrimSpace(rawField), `"'`)
					if fieldName != "" {
						schema.Properties[fieldName] = &observer.Schema{Type: "string"}
					}
				}
				result[currentName] = schema
				currentName = ""
			}
		}
	}
	return result, sc.Err()
}

// extractDjangoFile parses a urls.py file for Django routes.
func extractDjangoFile(path string, serializers map[string]*observer.Schema) ([]ScannedEndpoint, error) {
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
		// path("users/", View.as_view()) → GET + POST
		if m := reDjangoPath.FindStringSubmatch(line); m != nil {
			rawPath := "/" + strings.TrimLeft(m[1], "/")
			normalizedPath := normalizeDjangoPath(rawPath)
			for _, method := range []string{"GET", "POST"} {
				endpoints = append(endpoints, ScannedEndpoint{
					Method:      method,
					PathPattern: normalizedPath,
					Protocol:    "rest",
					Framework:   "django",
					SourceFile:  absPath,
					SourceLine:  i + 1,
				})
			}
		}

		// router.register("users", ViewSet) → full CRUD
		if m := reDjangoRouter.FindStringSubmatch(line); m != nil {
			base := "/" + strings.TrimLeft(m[1], "/")
			for _, c := range djangoCRUDMethods {
				endpoints = append(endpoints, ScannedEndpoint{
					Method:      c.method,
					PathPattern: base + c.suffix,
					Protocol:    "rest",
					Framework:   "django",
					SourceFile:  absPath,
					SourceLine:  i + 1,
				})
			}
		}
	}
	return endpoints, nil
}

// normalizeDjangoPath converts <int:pk> style params to {pk}.
func normalizeDjangoPath(path string) string {
	return reDjangoPathParam.ReplaceAllString(path, `{$1}`)
}
