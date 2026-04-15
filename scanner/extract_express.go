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

// expressExtractor implements Extractor for Express.js applications.
type expressExtractor struct{}

func (e *expressExtractor) Name() string { return "express" }

// Detect returns true if the directory contains a package.json with express as a dependency.
func (e *expressExtractor) Detect(dir string) bool {
	pkgPath := filepath.Join(dir, "package.json")
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), `"express"`)
}

// Express route patterns:
//
//	app.METHOD('/path', ...)
//	router.METHOD('/path', ...)
//
// Capture: (app|router|<any ident>).(get|post|put|patch|delete|head|options|all)('path', ...)
var reExpressRoute = regexp.MustCompile(
	`(?i)\b(?:app|router|\w+)\.(get|post|put|patch|delete|head|options|all)\s*\(\s*['"]([^'"]+)['"]`,
)

// Express app.use prefix: app.use('/prefix', router)
var reExpressUse = regexp.MustCompile(
	`(?i)\b(?:app|router|\w+)\.use\s*\(\s*['"]([^'"]+)['"]\s*,`,
)

// Zod object schema detection: z.object({ field: z.string(), ... })
var reZodObject = regexp.MustCompile(`z\.object\s*\(\s*\{([^}]+)\}`)

// Zod field: fieldName: z.TYPE()
var reZodField = regexp.MustCompile(`(\w+)\s*:\s*z\.(\w+)\s*\(`)

// TypeScript Request generic: Request<Params, ResBody, ReqBody>
// We capture the 3rd generic arg.
var reRequestGeneric = regexp.MustCompile(`Request\s*<[^,>]*,[^,>]*,\s*(\w+)`)

// JSDoc @deprecated tag
var reJSDocDeprecated = regexp.MustCompile(`@deprecated`)

// JSDoc @description or plain description line
var reJSDocDescription = regexp.MustCompile(`@description\s+(.+)`)

// skipExpressDirs are sub-directories to skip during Express scanning.
var skipExpressDirs = map[string]bool{
	"node_modules": true,
	"dist":         true,
	".next":        true,
	"coverage":     true,
	"vendor":       true,
	".git":         true,
	"__pycache__":  true,
}

// Extract walks dir and returns all discovered Express endpoints.
func (e *expressExtractor) Extract(dir string, cfg *config.ScanConfig) ([]ScannedEndpoint, error) {
	var endpoints []ScannedEndpoint
	err := walkJS(dir, skipExpressDirs, func(path string) error {
		found, err := extractExpressFile(path)
		if err != nil {
			// Log and continue — one bad file must not abort the scan.
			fmt.Fprintf(errorWriter, "scanner/express: error reading %s: %v\n", path, err)
			return nil
		}
		endpoints = append(endpoints, found...)
		return nil
	})
	return endpoints, err
}

// extractExpressFile parses a single JS/TS file for Express routes.
func extractExpressFile(path string) ([]ScannedEndpoint, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Detect JSDoc deprecated/description before any route.
	// We track the last JSDoc block ending just before each route line.
	jsdocBlocks := parseJSDocBlocks(lines)

	var endpoints []ScannedEndpoint
	absPath, _ := filepath.Abs(path)

	for i, line := range lines {
		lineNo := i + 1

		// Route match.
		m := reExpressRoute.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		method := strings.ToUpper(m[1])
		rawPath := m[2]

		normalizedPath := NormalizeFrameworkPath(rawPath)

		ep := ScannedEndpoint{
			Method:      method,
			PathPattern: normalizedPath,
			Protocol:    "rest",
			Framework:   "express",
			SourceFile:  absPath,
			SourceLine:  lineNo,
		}

		// Look for JSDoc block immediately preceding this line.
		if block, ok := jsdocBlocks[i]; ok {
			ep.Description = block.description
			ep.Deprecated = block.deprecated
		}

		// Best-effort: look for Zod schema or TypeScript generics in surrounding lines.
		ep.ReqSchema = extractExpressReqSchema(lines, i)

		endpoints = append(endpoints, ep)
	}

	return endpoints, nil
}

type jsdocInfo struct {
	description string
	deprecated  bool
}

// parseJSDocBlocks returns a map from the line index AFTER the closing */ to its jsdocInfo.
func parseJSDocBlocks(lines []string) map[int]jsdocInfo {
	result := make(map[int]jsdocInfo)
	inBlock := false
	var current jsdocInfo

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inBlock {
			if strings.HasPrefix(trimmed, "/**") {
				inBlock = true
				current = jsdocInfo{}
				// Check if the block opens and closes on the same line.
				if strings.Contains(trimmed[3:], "*/") {
					inBlock = false
					result[i+1] = current
				}
			}
		} else {
			if reJSDocDeprecated.MatchString(trimmed) {
				current.deprecated = true
			}
			if dm := reJSDocDescription.FindStringSubmatch(trimmed); dm != nil {
				current.description = strings.TrimSpace(dm[1])
			}
			if strings.HasSuffix(trimmed, "*/") || strings.Contains(trimmed, "*/") {
				inBlock = false
				result[i+1] = current
			}
		}
	}
	return result
}

// extractExpressReqSchema looks for a Zod schema or TypeScript Request generic
// in the lines around the route handler (up to 30 lines ahead).
func extractExpressReqSchema(lines []string, routeIdx int) *observer.Schema {
	end := routeIdx + 30
	if end > len(lines) {
		end = len(lines)
	}

	// Look for Zod object schema.
	for i := routeIdx; i < end; i++ {
		line := lines[i]
		if zm := reZodObject.FindStringSubmatch(line); zm != nil {
			schema := parseZodObject(zm[1])
			if schema != nil {
				return schema
			}
		}
	}

	// Look for TypeScript Request<P, ResBody, ReqBody> generic.
	for i := routeIdx; i < end; i++ {
		if rm := reRequestGeneric.FindStringSubmatch(lines[i]); rm != nil {
			typeName := rm[1]
			if typeName != "" && typeName != "any" && typeName != "unknown" {
				// Return a skeletal schema referencing the type name.
				return &observer.Schema{
					Type:        "object",
					Description: fmt.Sprintf("TS type: %s", typeName),
				}
			}
		}
	}

	return nil
}

// parseZodObject parses Zod field definitions from the interior of z.object({...}).
func parseZodObject(interior string) *observer.Schema {
	matches := reZodField.FindAllStringSubmatch(interior, -1)
	if len(matches) == 0 {
		return nil
	}
	schema := &observer.Schema{
		Type:       "object",
		Properties: make(map[string]*observer.Schema),
	}
	for _, m := range matches {
		fieldName := m[1]
		zodType := strings.ToLower(m[2])
		fieldSchema := zodTypeToSchema(zodType)
		schema.Properties[fieldName] = fieldSchema
	}
	return schema
}

// zodTypeToSchema converts a Zod primitive type name to an observer.Schema.
func zodTypeToSchema(zodType string) *observer.Schema {
	switch zodType {
	case "string":
		return &observer.Schema{Type: "string"}
	case "number":
		return &observer.Schema{Type: "number"}
	case "int", "integer":
		return &observer.Schema{Type: "integer"}
	case "boolean", "bool":
		return &observer.Schema{Type: "boolean"}
	case "array":
		return &observer.Schema{Type: "array"}
	case "object":
		return &observer.Schema{Type: "object"}
	case "date":
		return &observer.Schema{Type: "string", Format: "date-time"}
	default:
		return &observer.Schema{Type: "string"}
	}
}

// walkJS walks dir recursively, calling fn for every .js or .ts file.
// Skips directories listed in skipDirs.
func walkJS(dir string, skipSet map[string]bool, fn func(path string) error) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			if skipSet[name] {
				continue
			}
			if err := walkJS(filepath.Join(dir, name), skipSet, fn); err != nil {
				return err
			}
			continue
		}
		ext := filepath.Ext(name)
		if ext != ".js" && ext != ".ts" {
			continue
		}
		if err := fn(filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	return nil
}

