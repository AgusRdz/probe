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

// actixExtractor implements Extractor for Actix-Web applications.
type actixExtractor struct{}

func (e *actixExtractor) Name() string { return "actix" }

// Detect returns true if Cargo.toml mentions actix-web.
func (e *actixExtractor) Detect(dir string) bool {
	return cargoTomlContains(dir, "actix-web")
}

// Proc macro route attributes: #[get("/path")] #[post("/path")]
var reActixAttrRoute = regexp.MustCompile(
	`#\[(get|post|put|patch|delete|head|options)\s*\(\s*"([^"]+)"\s*\)\]`,
)

// web::scope("/prefix") — one level
var reActixScope = regexp.MustCompile(`web::scope\s*\(\s*"([^"]+)"\s*\)`)

// .service(web::resource("/path").route(web::get().to(handler)))
var reActixServiceResource = regexp.MustCompile(
	`web::resource\s*\(\s*"([^"]+)"\s*\).*?web\.(get|post|put|patch|delete)\(\)`,
)

// async fn handler(body: web::Json<TypeName>)
var reActixJsonParam = regexp.MustCompile(`web::Json\s*<(\w+)>`)

// Serde struct: struct TypeName {
var reRustStruct = regexp.MustCompile(`^(?:pub\s+)?struct\s+(\w+)\s*\{`)

// Struct field: name: Type,
var reRustField = regexp.MustCompile(`^\s+(?:pub\s+)?(\w+)\s*:\s*(Option<)?(\w+)`)

// #[serde(rename = "camelName")]
var reRustSerdeRename = regexp.MustCompile(`#\[serde\s*\([^)]*rename\s*=\s*"([^"]+)"`)

// #[serde(skip)]
var reRustSerdeSkip = regexp.MustCompile(`#\[serde\s*\([^)]*skip`)

// Extract walks dir and returns all discovered Actix endpoints.
func (e *actixExtractor) Extract(dir string, cfg *config.ScanConfig) ([]ScannedEndpoint, error) {
	// Collect Serde struct schemas.
	schemas := make(map[string]*observer.Schema)
	_ = walkRustFiles(dir, func(path string) error {
		found, ferr := extractRustStructSchemas(path)
		if ferr != nil {
			return nil
		}
		for k, v := range found {
			schemas[k] = v
		}
		return nil
	})

	var endpoints []ScannedEndpoint
	err := walkRustFiles(dir, func(path string) error {
		found, ferr := extractActixFile(path, schemas)
		if ferr != nil {
			fmt.Fprintf(errorWriter, "scanner/actix: error reading %s: %v\n", path, ferr)
			return nil
		}
		endpoints = append(endpoints, found...)
		return nil
	})
	return endpoints, err
}

// extractActixFile parses a .rs file for Actix route proc macros.
func extractActixFile(path string, schemas map[string]*observer.Schema) ([]ScannedEndpoint, error) {
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

	// Simple scope prefix — one level.
	prefix := ""
	for _, line := range lines {
		if m := reActixScope.FindStringSubmatch(line); m != nil {
			prefix = m[1]
			break
		}
	}

	var endpoints []ScannedEndpoint
	for i, line := range lines {
		// Proc-macro attribute route.
		if m := reActixAttrRoute.FindStringSubmatch(line); m != nil {
			method := strings.ToUpper(m[1])
			rawPath := normalizeActixPath(prefix + m[2])

			ep := ScannedEndpoint{
				Method:      method,
				PathPattern: rawPath,
				Protocol:    "rest",
				Framework:   "actix",
				SourceFile:  absPath,
				SourceLine:  i + 1,
			}

			// Look for handler signature in the next few lines.
			end := i + 5
			if end > len(lines) {
				end = len(lines)
			}
			for j := i + 1; j < end; j++ {
				if m2 := reActixJsonParam.FindStringSubmatch(lines[j]); m2 != nil {
					if s, ok := schemas[m2[1]]; ok {
						ep.ReqSchema = s
					}
					break
				}
			}

			endpoints = append(endpoints, ep)
			continue
		}

		// web::resource service pattern.
		if m := reActixServiceResource.FindStringSubmatch(line); m != nil {
			rawPath := normalizeActixPath(prefix + m[1])
			method := strings.ToUpper(m[2])
			endpoints = append(endpoints, ScannedEndpoint{
				Method:      method,
				PathPattern: rawPath,
				Protocol:    "rest",
				Framework:   "actix",
				SourceFile:  absPath,
				SourceLine:  i + 1,
			})
		}
	}
	return endpoints, nil
}

// extractRustStructSchemas collects Serde-annotated structs from a .rs file.
func extractRustStructSchemas(path string) (map[string]*observer.Schema, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	result := make(map[string]*observer.Schema)
	var currentStruct string
	var currentSchema *observer.Schema
	var pendingRename string
	var pendingSkip bool

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()

		if m := reRustStruct.FindStringSubmatch(line); m != nil {
			if currentStruct != "" && currentSchema != nil {
				result[currentStruct] = currentSchema
			}
			currentStruct = m[1]
			currentSchema = &observer.Schema{
				Type:       "object",
				Properties: make(map[string]*observer.Schema),
			}
			pendingRename = ""
			pendingSkip = false
			continue
		}

		if currentSchema == nil {
			continue
		}

		// Field-level serde attributes.
		if m := reRustSerdeRename.FindStringSubmatch(line); m != nil {
			pendingRename = m[1]
			continue
		}
		if reRustSerdeSkip.MatchString(line) {
			pendingSkip = true
			continue
		}

		// Struct closing brace.
		if strings.TrimSpace(line) == "}" {
			if currentStruct != "" {
				result[currentStruct] = currentSchema
			}
			currentStruct = ""
			currentSchema = nil
			continue
		}

		if m := reRustField.FindStringSubmatch(line); m != nil {
			if pendingSkip {
				pendingSkip = false
				pendingRename = ""
				continue
			}
			fieldName := m[1]
			if pendingRename != "" {
				fieldName = pendingRename
				pendingRename = ""
			}
			optional := m[2] != ""
			typeName := m[3]
			s := rustTypeToSchema(typeName)
			s.Nullable = optional
			currentSchema.Properties[fieldName] = &s
			if !optional {
				currentSchema.Required = append(currentSchema.Required, fieldName)
			}
			pendingSkip = false
		}
	}
	if currentStruct != "" && currentSchema != nil {
		result[currentStruct] = currentSchema
	}
	return result, sc.Err()
}

// rustTypeToSchema converts Rust type names to observer.Schema.
func rustTypeToSchema(typeName string) observer.Schema {
	switch typeName {
	case "String", "str":
		return observer.Schema{Type: "string"}
	case "u8", "u16", "u32", "u64", "u128", "usize",
		"i8", "i16", "i32", "i64", "i128", "isize":
		return observer.Schema{Type: "integer"}
	case "f32", "f64":
		return observer.Schema{Type: "number"}
	case "bool":
		return observer.Schema{Type: "boolean"}
	case "Vec":
		return observer.Schema{Type: "array"}
	default:
		return observer.Schema{Type: "object"}
	}
}

// normalizeActixPath strips regex suffixes and normalizes path params.
// /users/{id:\\d+} → /users/{id}
func normalizeActixPath(path string) string {
	// Strip regex inside path params: {id:\\d+} → {id}
	re := regexp.MustCompile(`\{(\w+):[^}]+\}`)
	path = re.ReplaceAllString(path, `{$1}`)
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	return path
}

// cargoTomlContains checks if Cargo.toml in dir mentions the given crate.
func cargoTomlContains(dir, crate string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "Cargo.toml"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), crate)
}

// walkRustFiles walks dir recursively, calling fn for every .rs file.
func walkRustFiles(dir string, fn func(string) error) error {
	return walkWithSkip(dir, map[string]bool{
		"target": true,
		".git":   true,
	}, ".rs", fn)
}
