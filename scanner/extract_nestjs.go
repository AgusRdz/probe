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

// nestjsExtractor implements Extractor for NestJS applications.
type nestjsExtractor struct{}

func (n *nestjsExtractor) Name() string { return "nestjs" }

// Detect returns true if the directory contains a package.json with @nestjs/core.
func (n *nestjsExtractor) Detect(dir string) bool {
	pkgPath := filepath.Join(dir, "package.json")
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), `"@nestjs/core"`)
}

// Regex patterns for NestJS controller and method decorators.
var (
	reNestController  = regexp.MustCompile(`@Controller\s*\(\s*['"]([^'"]*)['"]\s*\)`)
	reNestControllerB = regexp.MustCompile(`@Controller\s*\(\s*\)`) // no path
	reNestGet         = regexp.MustCompile(`@Get\s*\(\s*['"]?([^'")]*?)['"]?\s*\)`)
	reNestPost        = regexp.MustCompile(`@Post\s*\(\s*['"]?([^'")]*?)['"]?\s*\)`)
	reNestPut         = regexp.MustCompile(`@Put\s*\(\s*['"]?([^'")]*?)['"]?\s*\)`)
	reNestPatch       = regexp.MustCompile(`@Patch\s*\(\s*['"]?([^'")]*?)['"]?\s*\)`)
	reNestDelete      = regexp.MustCompile(`@Delete\s*\(\s*['"]?([^'")]*?)['"]?\s*\)`)
	reNestHead        = regexp.MustCompile(`@Head\s*\(\s*['"]?([^'")]*?)['"]?\s*\)`)

	reNestApiTags    = regexp.MustCompile(`@ApiTags\s*\(\s*['"]([^'"]+)['"]\s*\)`)
	reNestDeprecated = regexp.MustCompile(`(?i)@Deprecated\(\)|@ApiProperty\s*\([^)]*deprecated\s*:\s*true`)
	reNestBody       = regexp.MustCompile(`@Body\s*\(\s*[^)]*\)\s*\w+\s*:\s*(\w+)`)
	reNestParam      = regexp.MustCompile(`@Param\s*\(\s*['"](\w+)['"]\s*\)\s*\w+\s*:\s*(\w+)`)
	reNestQuery      = regexp.MustCompile(`@Query\s*\(\s*['"](\w+)['"]\s*\)\s*\w+\s*:\s*(\w+)`)

	// Class property pattern: propName: TypeName (with optional ? and decorators above).
	reClassProp      = regexp.MustCompile(`^\s+(\w+)\??\s*:\s*([\w\[\]]+)\s*[;=]?`)
	reIsEmail        = regexp.MustCompile(`@IsEmail\s*\(`)
	reIsUUID         = regexp.MustCompile(`@IsUUID\s*\(`)
	reIsOptional     = regexp.MustCompile(`@IsOptional\s*\(`)
	reIsArray        = regexp.MustCompile(`@IsArray\s*\(`)
	reMinLength      = regexp.MustCompile(`@MinLength\s*\(\s*(\d+)`)
	reMaxLength      = regexp.MustCompile(`@MaxLength\s*\(\s*(\d+)`)
	reApiPropDesc    = regexp.MustCompile(`@ApiProperty\s*\(\s*\{[^}]*description\s*:\s*['"]([^'"]+)['"]`)
	reNestReturnType = regexp.MustCompile(`Promise\s*<\s*(\w+)\s*>`)
)

// Extract walks dir and returns all discovered NestJS endpoints.
func (n *nestjsExtractor) Extract(dir string, cfg *config.ScanConfig) ([]ScannedEndpoint, error) {
	var endpoints []ScannedEndpoint

	skipSet := map[string]bool{
		"node_modules": true,
		"dist":         true,
		".next":        true,
		"coverage":     true,
		"vendor":       true,
		".git":         true,
	}

	// Walk .controller.ts and .controller.js files preferentially, but fall back to all TS/JS.
	err := walkJS(dir, skipSet, func(path string) error {
		base := filepath.Base(path)
		// Focus on controller files for efficiency, but also scan any .ts file.
		if !strings.Contains(base, ".controller.") && filepath.Ext(base) != ".ts" {
			return nil
		}
		found, err := extractNestJSFile(dir, path)
		if err != nil {
			fmt.Fprintf(errorWriter, "scanner/nestjs: error reading %s: %v\n", path, err)
			return nil
		}
		endpoints = append(endpoints, found...)
		return nil
	})
	return endpoints, err
}

// controllerState holds the state of the currently parsed controller class.
type controllerState struct {
	prefix     string
	tags       []string
	deprecated bool
}

// methodRouteInfo holds information gathered around a handler method.
type methodRouteInfo struct {
	httpMethod  string
	path        string
	body        string
	params      []ExtractedParam
	tags        []string
	deprecated  bool
	description string
	returnType  string
}

// extractNestJSFile parses a single file for NestJS controller routes.
func extractNestJSFile(rootDir, path string) ([]ScannedEndpoint, error) {
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

	// We do a single-pass scan. When we see @Controller we capture its prefix.
	// When we see @Get/@Post/... we record the route info and combine with the controller prefix.
	var ctrl *controllerState
	var pendingRoute *methodRouteInfo

	// Tags collected from @ApiTags at class level.
	var pendingTags []string
	var pendingDeprecated bool
	var pendingDescription string

	// Class-level type definitions keyed by class name.
	classDefs := extractClassDefs(lines)

	flushRoute := func(lineNo int) {
		if ctrl == nil || pendingRoute == nil {
			return
		}
		prefix := ctrl.prefix
		methodPath := pendingRoute.path

		combined := joinPaths(prefix, methodPath)
		combined = NormalizeFrameworkPath(combined)

		ep := ScannedEndpoint{
			Method:      pendingRoute.httpMethod,
			PathPattern: combined,
			Protocol:    "rest",
			Framework:   "nestjs",
			SourceFile:  absPath,
			SourceLine:  lineNo,
			Tags:        mergeTags(ctrl.tags, pendingRoute.tags),
			Deprecated:  ctrl.deprecated || pendingRoute.deprecated,
			Description: pendingRoute.description,
		}

		// Resolve @Body DTO class.
		if pendingRoute.body != "" {
			if classDef, ok := classDefs[pendingRoute.body]; ok {
				ep.ReqSchema = buildSchemaFromClassDef(classDef)
			} else {
				ep.ReqSchema = &observer.Schema{
					Type:        "object",
					Description: fmt.Sprintf("DTO: %s", pendingRoute.body),
				}
			}
		}

		// Resolve return type.
		if pendingRoute.returnType != "" {
			if classDef, ok := classDefs[pendingRoute.returnType]; ok {
				ep.RespSchema = buildSchemaFromClassDef(classDef)
			}
		}

		ep.Params = pendingRoute.params

		endpoints = append(endpoints, ep)
		pendingRoute = nil
	}

	for i, line := range lines {
		lineNo := i + 1
		trimmed := strings.TrimSpace(line)

		// @ApiTags at class level.
		if m := reNestApiTags.FindStringSubmatch(trimmed); m != nil {
			pendingTags = append(pendingTags, m[1])
		}

		// @Deprecated / @ApiProperty({deprecated: true})
		if reNestDeprecated.MatchString(trimmed) {
			pendingDeprecated = true
		}

		// JSDoc description.
		if dm := reJSDocDescription.FindStringSubmatch(trimmed); dm != nil {
			pendingDescription = strings.TrimSpace(dm[1])
		}

		// @Controller('/prefix') — start of a new controller.
		if m := reNestController.FindStringSubmatch(trimmed); m != nil {
			flushRoute(lineNo)
			ctrl = &controllerState{
				prefix:     m[1],
				tags:       pendingTags,
				deprecated: pendingDeprecated,
			}
			pendingTags = nil
			pendingDeprecated = false
			pendingDescription = ""
			continue
		}
		// @Controller() with no path.
		if reNestControllerB.MatchString(trimmed) && !reNestController.MatchString(trimmed) {
			flushRoute(lineNo)
			ctrl = &controllerState{
				tags:       pendingTags,
				deprecated: pendingDeprecated,
			}
			pendingTags = nil
			pendingDeprecated = false
			pendingDescription = ""
			continue
		}

		if ctrl == nil {
			continue
		}

		// HTTP method decorators.
		httpMethod, routePath := extractNestHTTPDecorator(trimmed)
		if httpMethod != "" {
			flushRoute(lineNo)
			pendingRoute = &methodRouteInfo{
				httpMethod:  httpMethod,
				path:        routePath,
				tags:        pendingTags,
				deprecated:  pendingDeprecated,
				description: pendingDescription,
			}
			pendingTags = nil
			pendingDeprecated = false
			pendingDescription = ""
			continue
		}

		if pendingRoute == nil {
			continue
		}

		// @Body() dto: ClassName
		if bm := reNestBody.FindStringSubmatch(trimmed); bm != nil {
			pendingRoute.body = bm[1]
		}

		// @Param('name') param: type
		if pm := reNestParam.FindStringSubmatch(trimmed); pm != nil {
			pendingRoute.params = append(pendingRoute.params, ExtractedParam{
				Name:     pm[1],
				In:       "path",
				Required: true,
				Schema:   tsTypeToSchema(pm[2]),
			})
		}

		// @Query('name') param: type
		if qm := reNestQuery.FindStringSubmatch(trimmed); qm != nil {
			pendingRoute.params = append(pendingRoute.params, ExtractedParam{
				Name:     qm[1],
				In:       "query",
				Required: false,
				Schema:   tsTypeToSchema(qm[2]),
			})
		}

		// Return type annotation: Promise<ClassName>
		if rm := reNestReturnType.FindStringSubmatch(trimmed); rm != nil {
			pendingRoute.returnType = rm[1]
		}

		// Flush on closing brace of a method (heuristic: lone "}" at method body close level).
		if trimmed == "}" {
			flushRoute(lineNo)
		}
	}

	// Flush any remaining pending route.
	flushRoute(len(lines))

	return endpoints, nil
}

// extractNestHTTPDecorator returns the HTTP method and path from a NestJS decorator line.
func extractNestHTTPDecorator(line string) (method, path string) {
	decorators := []struct {
		re     *regexp.Regexp
		method string
	}{
		{reNestGet, "GET"},
		{reNestPost, "POST"},
		{reNestPut, "PUT"},
		{reNestPatch, "PATCH"},
		{reNestDelete, "DELETE"},
		{reNestHead, "HEAD"},
	}
	for _, d := range decorators {
		if m := d.re.FindStringSubmatch(line); m != nil {
			return d.method, strings.TrimSpace(m[1])
		}
	}
	return "", ""
}

// classDef represents extracted property info for a TypeScript class.
type classDef struct {
	properties []classProperty
}

type classProperty struct {
	name        string
	tsType      string
	isOptional  bool
	isArray     bool
	format      string
	description string
	minLength   int
	maxLength   int
}

// reClassDecl matches a TypeScript class declaration.
var reClassDecl = regexp.MustCompile(`^(?:export\s+)?class\s+(\w+)`)

// extractClassDefs scans all lines and builds a map of class name → classDef.
func extractClassDefs(lines []string) map[string]classDef {
	defs := make(map[string]classDef)

	var currentClass string
	var currentDef classDef
	var pendingPropDecorators struct {
		isOptional  bool
		isEmail     bool
		isUUID      bool
		isArray     bool
		minLength   int
		maxLength   int
		description string
	}
	inClass := false
	braceDepth := 0

	resetDecorators := func() {
		pendingPropDecorators.isOptional = false
		pendingPropDecorators.isEmail = false
		pendingPropDecorators.isUUID = false
		pendingPropDecorators.isArray = false
		pendingPropDecorators.minLength = 0
		pendingPropDecorators.maxLength = 0
		pendingPropDecorators.description = ""
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if !inClass {
			if m := reClassDecl.FindStringSubmatch(trimmed); m != nil {
				currentClass = m[1]
				currentDef = classDef{}
				inClass = true
				braceDepth = 0
				resetDecorators()
			}
			continue
		}

		// Track brace depth to know when class ends.
		for _, ch := range trimmed {
			if ch == '{' {
				braceDepth++
			} else if ch == '}' {
				braceDepth--
			}
		}

		if braceDepth < 0 && inClass {
			defs[currentClass] = currentDef
			inClass = false
			currentClass = ""
			continue
		}

		// Property decorators.
		if reIsEmail.MatchString(trimmed) {
			pendingPropDecorators.isEmail = true
		}
		if reIsUUID.MatchString(trimmed) {
			pendingPropDecorators.isUUID = true
		}
		if reIsOptional.MatchString(trimmed) {
			pendingPropDecorators.isOptional = true
		}
		if reIsArray.MatchString(trimmed) {
			pendingPropDecorators.isArray = true
		}
		if mm := reMinLength.FindStringSubmatch(trimmed); mm != nil {
			parseInt(&pendingPropDecorators.minLength, mm[1])
		}
		if mm := reMaxLength.FindStringSubmatch(trimmed); mm != nil {
			parseInt(&pendingPropDecorators.maxLength, mm[1])
		}
		if mm := reApiPropDesc.FindStringSubmatch(trimmed); mm != nil {
			pendingPropDecorators.description = mm[1]
		}

		// Property definition.
		if pm := reClassProp.FindStringSubmatch(line); pm != nil {
			propName := pm[1]
			propType := pm[2]

			// Skip things that look like method signatures.
			if strings.HasPrefix(trimmed, "constructor") || strings.HasPrefix(trimmed, "async ") ||
				strings.Contains(trimmed, "(") {
				resetDecorators()
				continue
			}

			isOptional := pendingPropDecorators.isOptional || strings.HasSuffix(strings.Split(trimmed, ":")[0], "?")
			isArray := pendingPropDecorators.isArray || strings.HasSuffix(propType, "[]")

			prop := classProperty{
				name:        propName,
				tsType:      strings.TrimSuffix(propType, "[]"),
				isOptional:  isOptional,
				isArray:     isArray,
				description: pendingPropDecorators.description,
				minLength:   pendingPropDecorators.minLength,
				maxLength:   pendingPropDecorators.maxLength,
			}

			if pendingPropDecorators.isEmail {
				prop.format = "email"
			} else if pendingPropDecorators.isUUID {
				prop.format = "uuid"
			}

			currentDef.properties = append(currentDef.properties, prop)
			resetDecorators()
		}
	}

	// If file ended while inside a class.
	if inClass && currentClass != "" {
		defs[currentClass] = currentDef
	}

	return defs
}

// buildSchemaFromClassDef converts a classDef to an observer.Schema.
func buildSchemaFromClassDef(def classDef) *observer.Schema {
	schema := &observer.Schema{
		Type:       "object",
		Properties: make(map[string]*observer.Schema),
	}
	for _, prop := range def.properties {
		fieldSchema := tsTypeToSchemaFull(prop)
		schema.Properties[prop.name] = fieldSchema
		if !prop.isOptional {
			schema.Required = append(schema.Required, prop.name)
		}
	}
	if len(schema.Properties) == 0 {
		return schema
	}
	return schema
}

// tsTypeToSchemaFull converts a classProperty to an observer.Schema.
func tsTypeToSchemaFull(prop classProperty) *observer.Schema {
	base := tsTypeToSchema(prop.tsType)
	if prop.format != "" {
		base.Format = prop.format
	}
	if prop.description != "" {
		base.Description = prop.description
	}
	if prop.minLength > 0 {
		base.MinLength = prop.minLength
	}
	if prop.maxLength > 0 {
		base.MaxLength = prop.maxLength
	}
	if prop.isArray {
		baseCopy := base
		return &observer.Schema{
			Type:  "array",
			Items: &baseCopy,
		}
	}
	return &base
}

// tsTypeToSchema maps a TypeScript type name to an observer.Schema.
func tsTypeToSchema(tsType string) observer.Schema {
	switch strings.ToLower(tsType) {
	case "string":
		return observer.Schema{Type: "string"}
	case "number":
		return observer.Schema{Type: "number"}
	case "integer", "int":
		return observer.Schema{Type: "integer"}
	case "boolean", "bool":
		return observer.Schema{Type: "boolean"}
	case "any", "object", "record":
		return observer.Schema{Type: "object"}
	case "date":
		return observer.Schema{Type: "string", Format: "date-time"}
	default:
		return observer.Schema{Type: "object"}
	}
}

// joinPaths combines a controller prefix with a method path.
func joinPaths(prefix, methodPath string) string {
	if prefix == "" {
		return "/" + strings.TrimPrefix(methodPath, "/")
	}
	p := "/" + strings.Trim(prefix, "/")
	if methodPath == "" {
		return p
	}
	return p + "/" + strings.TrimPrefix(methodPath, "/")
}

// mergeTags combines two tag slices, deduplicating.
func mergeTags(a, b []string) []string {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	var result []string
	for _, t := range append(a, b...) {
		if !seen[t] {
			seen[t] = true
			result = append(result, t)
		}
	}
	return result
}

// parseInt parses a decimal string into *int, ignoring errors.
func parseInt(dst *int, s string) {
	var v int
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return
		}
		v = v*10 + int(ch-'0')
	}
	*dst = v
}
