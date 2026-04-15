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

// ktorExtractor implements Extractor for Ktor Kotlin applications.
type ktorExtractor struct{}

func (e *ktorExtractor) Name() string { return "ktor" }

// Detect returns true if build.gradle or build.gradle.kts mentions ktor.
func (e *ktorExtractor) Detect(dir string) bool {
	for _, fname := range []string{"build.gradle", "build.gradle.kts"} {
		data, err := os.ReadFile(filepath.Join(dir, fname))
		if err != nil {
			continue
		}
		if strings.Contains(strings.ToLower(string(data)), "ktor") {
			return true
		}
	}
	return false
}

// get("/path") { ... }  or  post("/path") { ... }
var reKtorRoute = regexp.MustCompile(
	`\b(get|post|put|patch|delete|head|options)\s*\(\s*"([^"]+)"\s*\)`,
)

// route("/prefix") { ... }
var reKtorRouteBlock = regexp.MustCompile(
	`\broute\s*\(\s*"([^"]+)"\s*\)`,
)

// call.receive<TypeName>()
var reKtorReceive = regexp.MustCompile(`call\.receive\s*<(\w+)>\s*\(`)

// Kotlin data class: data class TypeName(
var reKtorDataClass = regexp.MustCompile(`^(?:data\s+)?class\s+(\w+)\s*\(`)

// Kotlin constructor param: val name: Type = default or val name: Type?
var reKtorParam = regexp.MustCompile(`(?:val|var)\s+(\w+)\s*:\s*([\w?]+)(?:\s*=\s*(.+))?`)

// {param?} optional path param in Ktor: normalize to {param}
var reKtorOptionalParam = regexp.MustCompile(`\{(\w+)\?}`)

// Extract walks dir and returns all discovered Ktor endpoints.
func (e *ktorExtractor) Extract(dir string, cfg *config.ScanConfig) ([]ScannedEndpoint, error) {
	// Collect Kotlin data class schemas.
	schemas := make(map[string]*observer.Schema)
	_ = walkKotlinFiles(dir, func(path string) error {
		found, ferr := extractKotlinDataClasses(path)
		if ferr != nil {
			return nil
		}
		for k, v := range found {
			schemas[k] = v
		}
		return nil
	})

	var endpoints []ScannedEndpoint
	err := walkKotlinFiles(dir, func(path string) error {
		found, ferr := extractKtorFile(path, schemas)
		if ferr != nil {
			fmt.Fprintf(errorWriter, "scanner/ktor: error reading %s: %v\n", path, ferr)
			return nil
		}
		endpoints = append(endpoints, found...)
		return nil
	})
	return endpoints, err
}

// extractKtorFile parses a .kt file for Ktor route registrations.
func extractKtorFile(path string, schemas map[string]*observer.Schema) ([]ScannedEndpoint, error) {
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

	// Simple prefix stack.
	prefixStack := []string{""}
	push := func(seg string) {
		top := prefixStack[len(prefixStack)-1]
		prefixStack = append(prefixStack, top+seg)
	}
	pop := func() {
		if len(prefixStack) > 1 {
			prefixStack = prefixStack[:len(prefixStack)-1]
		}
	}
	top := func() string { return prefixStack[len(prefixStack)-1] }

	// Track brace depth for prefix scope management.
	braceDepthAtPush := []int{}
	braceDepth := 0

	var endpoints []ScannedEndpoint
	for i, line := range lines {
		// Count braces.
		for _, c := range line {
			switch c {
			case '{':
				braceDepth++
			case '}':
				braceDepth--
				// Pop prefix if we've closed the corresponding block.
				if len(braceDepthAtPush) > 0 {
					if braceDepth < braceDepthAtPush[len(braceDepthAtPush)-1] {
						pop()
						braceDepthAtPush = braceDepthAtPush[:len(braceDepthAtPush)-1]
					}
				}
			}
		}

		// Prefix block: route("/prefix") {
		if m := reKtorRouteBlock.FindStringSubmatch(line); m != nil {
			push(m[1])
			braceDepthAtPush = append(braceDepthAtPush, braceDepth-1)
			continue
		}

		// Route handler: get("/path") { ... }
		if m := reKtorRoute.FindStringSubmatch(line); m != nil {
			method := strings.ToUpper(m[1])
			rawPath := m[2]
			fullPath := normalizeKtorPath(top() + rawPath)

			ep := ScannedEndpoint{
				Method:      method,
				PathPattern: fullPath,
				Protocol:    "rest",
				Framework:   "ktor",
				SourceFile:  absPath,
				SourceLine:  i + 1,
			}

			// Look for call.receive<T>() in the next few lines.
			end := i + 10
			if end > len(lines) {
				end = len(lines)
			}
			for j := i + 1; j < end; j++ {
				if m2 := reKtorReceive.FindStringSubmatch(lines[j]); m2 != nil {
					if s, ok := schemas[m2[1]]; ok {
						ep.ReqSchema = s
					}
					break
				}
			}

			endpoints = append(endpoints, ep)
		}
	}
	return endpoints, nil
}

// extractKotlinDataClasses parses a .kt file for data class definitions.
func extractKotlinDataClasses(path string) (map[string]*observer.Schema, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	result := make(map[string]*observer.Schema)
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	for i, line := range lines {
		m := reKtorDataClass.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		className := m[1]
		schema := &observer.Schema{
			Type:       "object",
			Properties: make(map[string]*observer.Schema),
		}

		// Parse constructor parameters — may span multiple lines.
		// Collect until closing ')'.
		buf := line
		for j := i + 1; j < len(lines) && !strings.Contains(buf, ")"); j++ {
			buf += " " + lines[j]
		}
		// Extract all val/var params.
		for _, pm := range reKtorParam.FindAllStringSubmatch(buf, -1) {
			paramName := pm[1]
			paramType := strings.TrimSuffix(pm[2], "?")
			hasDefault := pm[3] != ""
			optional := strings.HasSuffix(pm[2], "?") || hasDefault

			s := kotlinTypeToSchema(paramType)
			s.Nullable = strings.HasSuffix(pm[2], "?")
			schema.Properties[paramName] = &s
			if !optional {
				schema.Required = append(schema.Required, paramName)
			}
		}
		result[className] = schema
	}
	return result, nil
}

// kotlinTypeToSchema converts Kotlin type names to observer.Schema.
func kotlinTypeToSchema(typeName string) observer.Schema {
	switch typeName {
	case "String":
		return observer.Schema{Type: "string"}
	case "Int", "Long", "Short", "Byte":
		return observer.Schema{Type: "integer"}
	case "Float", "Double":
		return observer.Schema{Type: "number"}
	case "Boolean":
		return observer.Schema{Type: "boolean"}
	case "List", "MutableList", "ArrayList", "Set":
		return observer.Schema{Type: "array"}
	default:
		return observer.Schema{Type: "object"}
	}
}

// normalizeKtorPath converts {param?} to {param} and cleans double slashes.
func normalizeKtorPath(path string) string {
	path = reKtorOptionalParam.ReplaceAllString(path, `{$1}`)
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

// walkKotlinFiles walks dir recursively, calling fn for every .kt file.
func walkKotlinFiles(dir string, fn func(string) error) error {
	return walkWithSkip(dir, map[string]bool{
		"build":  true,
		".git":   true,
		".gradle": true,
	}, ".kt", fn)
}
