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

// aspnetMVCExtractor implements Extractor for ASP.NET Core MVC (Controller-based) apps.
type aspnetMVCExtractor struct{}

func (e *aspnetMVCExtractor) Name() string { return "aspnet-mvc" }

// Detect returns true if the directory contains a .csproj file.
func (e *aspnetMVCExtractor) Detect(dir string) bool {
	return csprojExists(dir)
}

// [Route("api/[controller]")] attribute on class
var reCSRouteAttr = regexp.MustCompile(`\[Route\s*\(\s*"([^"]+)"\s*\)\]`)

// [RoutePrefix("api/account")] — .NET Framework Web API class-level prefix
var reCSRoutePrefixAttr = regexp.MustCompile(`\[RoutePrefix\s*\(\s*"([^"]+)"\s*\)\]`)

// [HttpGet], [HttpPost], [HttpPut], [HttpPatch], [HttpDelete] on method
var reCSHttpMethod = regexp.MustCompile(`\[Http(Get|Post|Put|Patch|Delete|Head|Options)(?:\s*\(\s*"([^"]*)"\s*\))?\]`)

// class UsersController : ...
var reCSController = regexp.MustCompile(`(?:public\s+)?(?:abstract\s+)?class\s+(\w+Controller)\s*(?::|$)`)

// public ActionResult / IActionResult / IHttpActionResult / Task<> method signature
// Handles .NET Core (IActionResult, ActionResult<T>) and .NET Framework (IHttpActionResult, HttpResponseMessage).
// Group 1: inner type from \w+Result<T>; Group 2: inner type from ActionResult<T> (may be complex); Group 3: method name.
var reCSActionMethod = regexp.MustCompile(`(?i)public\s+(?:async\s+)?(?:Task<)?(?:\w+Result(?:<(\w+)>)?|IActionResult|IHttpActionResult|HttpResponseMessage|ActionResult(?:<(\w+(?:<[^>]+>)?)>)?)>?\s+(\w+)\s*\(`)

// [FromBody] TypeName paramName
var reCSFromBody = regexp.MustCompile(`\[FromBody\]\s+(\w+)\s+\w+`)

// C# property: public string Name { get; set; }
var reCSProperty = regexp.MustCompile(`public\s+(\w[\w?<>\[\]]*)\s+(\w+)\s*\{\s*get`)

// [Required] attribute
var reCSRequired = regexp.MustCompile(`\[Required\]`)

// [EmailAddress] attribute
var reCSEmailAddress = regexp.MustCompile(`\[EmailAddress\]`)

// [Obsolete] attribute — maps to deprecated
var reCSObsolete = regexp.MustCompile(`\[Obsolete`)

// /// <summary>...</summary>
var reCSXmlSummary = regexp.MustCompile(`///\s*<summary>\s*(.+)`)

// C# class or record declaration.
var reCSClassDecl = regexp.MustCompile(`(?:public\s+)?(?:class|record)\s+(\w+)`)

// Extract walks dir and returns all discovered ASP.NET MVC endpoints.
func (e *aspnetMVCExtractor) Extract(dir string, cfg *config.ScanConfig) ([]ScannedEndpoint, error) {
	// First pass: collect C# class schemas across all .cs files.
	schemas := make(map[string]*observer.Schema)
	_ = walkCSharpFiles(dir, func(path string) error {
		found, ferr := extractCSTypeSchemas(path)
		if ferr != nil {
			return nil
		}
		for k, v := range found {
			schemas[k] = v
		}
		return nil
	})

	var endpoints []ScannedEndpoint
	err := walkCSharpFiles(dir, func(path string) error {
		base := filepath.Base(path)
		if !strings.HasSuffix(base, "Controller.cs") {
			return nil
		}
		found, ferr := extractASPNetMVCFile(path, schemas)
		if ferr != nil {
			fmt.Fprintf(errorWriter, "scanner/aspnet-mvc: error reading %s: %v\n", path, ferr)
			return nil
		}
		endpoints = append(endpoints, found...)
		return nil
	})
	return endpoints, err
}

// extractASPNetMVCFile parses a C# controller file for route endpoints.
func extractASPNetMVCFile(path string, schemas map[string]*observer.Schema) ([]ScannedEndpoint, error) {
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

	// Extract class-level route and controller name.
	// Two-pass: first collect controllerBasePath, then resolve [controller] in route.
	classPrefix := ""
	controllerBasePath := ""
	deprecated := false
	for _, line := range lines {
		if m := reCSController.FindStringSubmatch(line); m != nil {
			controllerBasePath = "/" + strings.ToLower(strings.TrimSuffix(m[1], "Controller"))
			break
		}
	}
	classPrefixLine := -1
	for idx, line := range lines {
		// Check both [Route("...")] and [RoutePrefix("...")] for class-level prefix.
		raw := ""
		if m := reCSRoutePrefixAttr.FindStringSubmatch(line); m != nil {
			raw = m[1]
		} else if m := reCSRouteAttr.FindStringSubmatch(line); m != nil {
			raw = m[1]
		}
		if raw != "" {
			raw = strings.Replace(raw, "[controller]",
				strings.TrimPrefix(controllerBasePath, "/"), 1)
			classPrefix = "/" + strings.TrimLeft(raw, "/")
			classPrefixLine = idx
			break
		}
	}

	if classPrefix == "" {
		classPrefix = controllerBasePath
	}

	var endpoints []ScannedEndpoint
	var pendingMethod string
	var pendingMethodPath string  // from [HttpMethod("path")]
	var pendingRoutePath string   // from method-level [Route("path")]
	var pendingDeprecated bool
	var pendingDescription string

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Track [Obsolete] for next method.
		if reCSObsolete.MatchString(trimmed) {
			pendingDeprecated = true
			continue
		}

		// Track /// <summary> description.
		if m := reCSXmlSummary.FindStringSubmatch(trimmed); m != nil {
			pendingDescription = strings.TrimSpace(m[1])
			continue
		}

		// [HttpMethod] or [HttpMethod("path")]
		if m := reCSHttpMethod.FindStringSubmatch(trimmed); m != nil {
			pendingMethod = strings.ToUpper(m[1])
			pendingMethodPath = m[2] // may be empty
			continue
		}

		// Method-level [Route("path")] — skip the line that already set the class prefix.
		// Handles both orders: [HttpGet] then [Route], or [Route] then [HttpGet].
		if m := reCSRouteAttr.FindStringSubmatch(trimmed); m != nil && i != classPrefixLine {
			pendingRoutePath = m[1]
			continue
		}

		// Action method signature.
		if pendingMethod != "" {
			if m := reCSActionMethod.FindStringSubmatch(trimmed); m != nil {
				responseType := m[1]
				if responseType == "" {
					responseType = m[2]
				}

				// Use explicit method path, fall back to method-level [Route], then empty.
				methodPath := pendingMethodPath
				if methodPath == "" {
					methodPath = pendingRoutePath
				}
				fullPath := NormalizeFrameworkPath(classPrefix + "/" + strings.TrimLeft(methodPath, "/"))
				// Collapse double slashes.
				for strings.Contains(fullPath, "//") {
					fullPath = strings.ReplaceAll(fullPath, "//", "/")
				}
				if fullPath == "" {
					fullPath = "/"
				}
				// Strip trailing slash (except root).
				if len(fullPath) > 1 {
					fullPath = strings.TrimRight(fullPath, "/")
				}
				// Strip leading slash for consistent path format.
				fullPath = strings.TrimLeft(fullPath, "/")

				ep := ScannedEndpoint{
					Method:      pendingMethod,
					PathPattern: fullPath,
					Protocol:    "rest",
					Framework:   "aspnet-mvc",
					SourceFile:  absPath,
					SourceLine:  i + 1,
					Deprecated:  pendingDeprecated || deprecated,
					Description: pendingDescription,
				}

				// Resolve response schema.
				if responseType != "" {
					if s, ok := schemas[responseType]; ok {
						ep.RespSchema = s
					}
				}

				// Look for [FromBody] param in method signature (same line).
				if m2 := reCSFromBody.FindStringSubmatch(trimmed); m2 != nil {
					if s, ok := schemas[m2[1]]; ok {
						ep.ReqSchema = s
					}
				}

				endpoints = append(endpoints, ep)
				pendingMethod = ""
				pendingMethodPath = ""
				pendingRoutePath = ""
				pendingDeprecated = false
				pendingDescription = ""
			}
		}
	}

	_ = deprecated
	return endpoints, nil
}

// extractCSTypeSchemas collects C# class/record property schemas.
func extractCSTypeSchemas(path string) (map[string]*observer.Schema, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	result := make(map[string]*observer.Schema)
	var currentClass string
	var currentSchema *observer.Schema
	var pendingRequired bool
	var pendingEmail bool
	var pendingObsolete bool

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)

		// Class or record declaration.
		if m := reCSClassDecl.FindStringSubmatch(trimmed); m != nil {
			if currentClass != "" && currentSchema != nil {
				result[currentClass] = currentSchema
			}
			currentClass = m[1]
			currentSchema = &observer.Schema{
				Type:       "object",
				Properties: make(map[string]*observer.Schema),
			}
			continue
		}

		if currentSchema == nil {
			continue
		}

		if reCSRequired.MatchString(trimmed) {
			pendingRequired = true
		}
		if reCSEmailAddress.MatchString(trimmed) {
			pendingEmail = true
		}
		if reCSObsolete.MatchString(trimmed) {
			pendingObsolete = true
		}

		if m := reCSProperty.FindStringSubmatch(trimmed); m != nil {
			csType := m[1]
			propName := m[2]
			s := csTypeToSchema(csType)
			if pendingEmail {
				s.Format = "email"
			}
			if pendingObsolete {
				// Mark as deprecated — stored in Description for now.
				s.Description = "deprecated"
			}
			currentSchema.Properties[propName] = &s
			if pendingRequired || (!strings.HasSuffix(csType, "?") && !strings.Contains(csType, "List<")) {
				currentSchema.Required = append(currentSchema.Required, propName)
			}
			pendingRequired = false
			pendingEmail = false
			pendingObsolete = false
		}
	}
	if currentClass != "" && currentSchema != nil {
		result[currentClass] = currentSchema
	}
	return result, sc.Err()
}

// csTypeToSchema converts a C# type annotation to observer.Schema.
func csTypeToSchema(csType string) observer.Schema {
	nullable := strings.HasSuffix(csType, "?")
	base := strings.TrimSuffix(csType, "?")

	s := observer.Schema{Nullable: nullable}
	switch base {
	case "string", "String":
		s.Type = "string"
	case "int", "Int32", "long", "Int64", "short", "byte":
		s.Type = "integer"
	case "double", "float", "decimal", "Double", "Float", "Decimal":
		s.Type = "number"
	case "bool", "Boolean":
		s.Type = "boolean"
	default:
		if strings.HasPrefix(base, "List<") || strings.HasSuffix(base, "[]") {
			s.Type = "array"
		} else {
			s.Type = "object"
		}
	}
	return s
}

// csprojExists returns true if any .csproj file exists in dir.
func csprojExists(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".csproj") {
			return true
		}
	}
	return false
}

// walkCSharpFiles walks dir recursively, calling fn for every .cs file.
func walkCSharpFiles(dir string, fn func(string) error) error {
	return walkWithSkip(dir, map[string]bool{
		"obj":        true,
		"bin":        true,
		".git":       true,
		"node_modules": true,
	}, ".cs", fn)
}
