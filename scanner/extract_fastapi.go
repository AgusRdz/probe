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

// fastAPIExtractor implements Extractor for FastAPI applications.
type fastAPIExtractor struct{}

func (e *fastAPIExtractor) Name() string { return "fastapi" }

// Detect returns true if requirements.txt or pyproject.toml mentions fastapi.
func (e *fastAPIExtractor) Detect(dir string) bool {
	return pyProjectContains(dir, "fastapi")
}

// FastAPI route decorator: @app.get("/path") or @router.post("/path", ...)
var reFastAPIRoute = regexp.MustCompile(
	`(?i)@(?:app|router|\w+)\.(get|post|put|patch|delete|head|options)\s*\(\s*["']([^"']+)["']`,
)

// APIRouter prefix: router = APIRouter(prefix="/prefix")
var reFastAPIRouterPrefix = regexp.MustCompile(
	`(?i)APIRouter\s*\([^)]*prefix\s*=\s*["']([^"']+)["']`,
)

// response_model= in route decorator
var reFastAPIResponseModel = regexp.MustCompile(`response_model\s*=\s*(\w+)`)

// async/sync def endpoint(body: TypeName):
var reFastAPIBodyParam = regexp.MustCompile(`(?:async\s+)?def\s+\w+\s*\([^)]*\b(\w+)\s*:\s*(\w+)[^)]*\)`)

// class ClassName(BaseModel): — Pydantic model
var reFastAPIModel = regexp.MustCompile(`^class\s+(\w+)\s*\(\s*BaseModel\s*\)\s*:`)

// Pydantic field: name: type or name: Optional[type]
var reFastAPIField = regexp.MustCompile(`^\s{4}(\w+)\s*:\s*(Optional\[)?(\w+)`)

// FastAPI path param is already {param} — no transform needed.

// Depends(...) patterns that indicate authentication requirements.
var reFastAPIAuthDepend = regexp.MustCompile(`Depends\s*\(\s*(?:get_current_user|oauth2_scheme|security|get_current_active_user|verify_token|authenticate)`)

// Extract walks dir and returns all discovered FastAPI endpoints.
func (e *fastAPIExtractor) Extract(dir string, cfg *config.ScanConfig) ([]ScannedEndpoint, error) {
	// First pass: collect all Pydantic models across all .py files.
	models := make(map[string]*observer.Schema)
	err := walkPythonFiles(dir, func(path string) error {
		found, ferr := extractFastAPIModels(path)
		if ferr != nil {
			fmt.Fprintf(errorWriter, "scanner/fastapi: error reading models %s: %v\n", path, ferr)
			return nil
		}
		for k, v := range found {
			models[k] = v
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Second pass: extract routes.
	var endpoints []ScannedEndpoint
	err = walkPythonFiles(dir, func(path string) error {
		found, ferr := extractFastAPIFile(path, models)
		if ferr != nil {
			fmt.Fprintf(errorWriter, "scanner/fastapi: error reading %s: %v\n", path, ferr)
			return nil
		}
		endpoints = append(endpoints, found...)
		return nil
	})
	return endpoints, err
}

// extractFastAPIModels scans a .py file for Pydantic BaseModel subclasses.
func extractFastAPIModels(path string) (map[string]*observer.Schema, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	models := make(map[string]*observer.Schema)
	var currentModel string
	var currentSchema *observer.Schema

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()

		if m := reFastAPIModel.FindStringSubmatch(line); m != nil {
			// Save previous model.
			if currentModel != "" && currentSchema != nil {
				models[currentModel] = currentSchema
			}
			currentModel = m[1]
			currentSchema = &observer.Schema{
				Type:       "object",
				Properties: make(map[string]*observer.Schema),
			}
			continue
		}

		if currentSchema != nil {
			if m := reFastAPIField.FindStringSubmatch(line); m != nil {
				fieldName := m[1]
				optional := m[2] != ""
				typeName := m[3]
				fieldSchema := pyTypeToSchema(typeName)
				currentSchema.Properties[fieldName] = &fieldSchema
				if !optional && !strings.Contains(line, "= None") && !strings.Contains(line, "Field(default=") {
					currentSchema.Required = append(currentSchema.Required, fieldName)
				}
			} else if line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
				// Non-indented line ends the class.
				if currentModel != "" {
					models[currentModel] = currentSchema
				}
				currentModel = ""
				currentSchema = nil
			}
		}
	}
	if currentModel != "" && currentSchema != nil {
		models[currentModel] = currentSchema
	}
	return models, scanner.Err()
}

// extractFastAPIFile parses a single .py file for FastAPI routes.
func extractFastAPIFile(path string, models map[string]*observer.Schema) ([]ScannedEndpoint, error) {
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

	// Detect module-level router prefix.
	prefix := ""
	for _, line := range lines {
		if m := reFastAPIRouterPrefix.FindStringSubmatch(line); m != nil {
			prefix = m[1]
			break
		}
	}

	var endpoints []ScannedEndpoint
	for i, line := range lines {
		m := reFastAPIRoute.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		method := strings.ToUpper(m[1])
		rawPath := m[2]
		fullPath := NormalizeFrameworkPath(prefix + rawPath)

		ep := ScannedEndpoint{
			Method:      method,
			PathPattern: fullPath,
			Protocol:    "rest",
			Framework:   "fastapi",
			SourceFile:  absPath,
			SourceLine:  i + 1,
		}

		// Look for response_model in the same decorator line.
		if rm := reFastAPIResponseModel.FindStringSubmatch(line); rm != nil {
			if s, ok := models[rm[1]]; ok {
				ep.RespSchema = s
			}
		}

		// Look for function signature and docstring in next ~10 lines.
		end := i + 10
		if end > len(lines) {
			end = len(lines)
		}
		for j := i + 1; j < end; j++ {
			// Function definition line — extract body param type.
			if m2 := reFastAPIBodyParam.FindStringSubmatch(lines[j]); m2 != nil {
				typeName := m2[2]
				if s, ok := models[typeName]; ok {
					ep.ReqSchema = s
				}
			}
			// Auth dependency in function signature.
			if reFastAPIAuthDepend.MatchString(lines[j]) {
				ep.RequiresAuth = true
			}
			// Docstring — first triple-quoted line.
			if ep.Description == "" {
				trimmed := strings.TrimSpace(lines[j])
				if strings.HasPrefix(trimmed, `"""`) {
					desc := strings.TrimPrefix(trimmed, `"""`)
					desc = strings.TrimSuffix(desc, `"""`)
					desc = strings.TrimSpace(desc)
					if desc != "" {
						ep.Description = desc
					}
				}
			}
		}

		endpoints = append(endpoints, ep)
	}
	return endpoints, nil
}

// pyTypeToSchema converts Python/Pydantic type names to observer.Schema.
func pyTypeToSchema(typeName string) observer.Schema {
	switch typeName {
	case "str", "EmailStr", "HttpUrl", "AnyUrl":
		s := observer.Schema{Type: "string"}
		if typeName == "EmailStr" {
			s.Format = "email"
		}
		return s
	case "int":
		return observer.Schema{Type: "integer"}
	case "float":
		return observer.Schema{Type: "number"}
	case "bool":
		return observer.Schema{Type: "boolean"}
	case "List", "list":
		return observer.Schema{Type: "array", Items: &observer.Schema{Type: "string"}}
	case "dict", "Dict":
		return observer.Schema{Type: "object"}
	default:
		return observer.Schema{Type: "string"}
	}
}

// pyProjectContains checks if requirements.txt or pyproject.toml in dir mentions pkg.
func pyProjectContains(dir, pkg string) bool {
	for _, fname := range []string{"requirements.txt", "pyproject.toml"} {
		data, err := os.ReadFile(filepath.Join(dir, fname))
		if err != nil {
			continue
		}
		if strings.Contains(strings.ToLower(string(data)), strings.ToLower(pkg)) {
			return true
		}
	}
	return false
}

// walkPythonFiles walks dir recursively, calling fn for every .py file.
func walkPythonFiles(dir string, fn func(string) error) error {
	return walkWithSkip(dir, map[string]bool{
		".venv":       true,
		"venv":        true,
		"__pycache__": true,
		".git":        true,
		"node_modules": true,
	}, ".py", fn)
}
