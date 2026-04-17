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

// springExtractor implements Extractor for Spring Boot applications.
type springExtractor struct{}

func (e *springExtractor) Name() string { return "spring" }

// Detect returns true if pom.xml or build.gradle mentions spring-boot.
func (e *springExtractor) Detect(dir string) bool {
	for _, fname := range []string{"pom.xml", "build.gradle", "build.gradle.kts"} {
		data, err := os.ReadFile(filepath.Join(dir, fname))
		if err != nil {
			continue
		}
		if strings.Contains(strings.ToLower(string(data)), "spring-boot") {
			return true
		}
	}
	return false
}

// @RestController annotation
var reSpringRestController = regexp.MustCompile(`@RestController`)

// @RequestMapping("/prefix") on class
var reSpringRequestMapping = regexp.MustCompile(`@RequestMapping\s*\(\s*(?:value\s*=\s*)?["']([^"']+)["']`)

// @GetMapping, @PostMapping etc. on method
var reSpringMethodMapping = regexp.MustCompile(
	`@(Get|Post|Put|Patch|Delete)Mapping\s*(?:\(\s*(?:value\s*=\s*)?["']([^"']+)["']\s*\))?`,
)

// public ResponseEntity<TypeName> methodName(
var reSpringActionMethod = regexp.MustCompile(
	`public\s+(?:ResponseEntity|(?:\w+))<(\w+)>\s+(\w+)\s*\(`,
)

// @RequestBody TypeName paramName
var reSpringRequestBody = regexp.MustCompile(`@RequestBody\s+(\w+)\s+\w+`)

// Java class field: private String name;
var reJavaField = regexp.MustCompile(`^\s+private\s+(\w[\w<>[\]]*)\s+(\w+)\s*;`)

// @NotNull annotation
var reJavaNotNull = regexp.MustCompile(`@NotNull`)

// @Email annotation
var reJavaEmailAnnotation = regexp.MustCompile(`@Email`)

// @Deprecated annotation
var reJavaDeprecated = regexp.MustCompile(`@Deprecated`)

// @PreAuthorize, @Secured, @RolesAllowed — require authentication
var reSpringPreAuthorize = regexp.MustCompile(`@PreAuthorize\s*\(`)
var reSpringSecured       = regexp.MustCompile(`@Secured\s*\(`)
var reSpringRolesAllowed  = regexp.MustCompile(`@RolesAllowed\s*\(`)

// @Size(min=N, max=M)
var reJavaSize = regexp.MustCompile(`@Size\s*\([^)]*min\s*=\s*(\d+)[^)]*max\s*=\s*(\d+)`)

// /** Javadoc first line */
var reJavadoc = regexp.MustCompile(`/\*\*\s*(.+)`)

// Extract walks dir and returns all discovered Spring Boot endpoints.
func (e *springExtractor) Extract(dir string, cfg *config.ScanConfig) ([]ScannedEndpoint, error) {
	// First pass: collect Java class schemas.
	schemas := make(map[string]*observer.Schema)
	_ = walkJavaFiles(dir, func(path string) error {
		found, ferr := extractJavaTypeSchemas(path)
		if ferr != nil {
			return nil
		}
		for k, v := range found {
			schemas[k] = v
		}
		return nil
	})

	var endpoints []ScannedEndpoint
	err := walkJavaFiles(dir, func(path string) error {
		base := filepath.Base(path)
		if !strings.HasSuffix(base, "Controller.java") {
			return nil
		}
		found, ferr := extractSpringFile(path, schemas)
		if ferr != nil {
			fmt.Fprintf(errorWriter, "scanner/spring: error reading %s: %v\n", path, ferr)
			return nil
		}
		endpoints = append(endpoints, found...)
		return nil
	})
	return endpoints, err
}

// extractSpringFile parses a Java controller file for Spring routes.
func extractSpringFile(path string, schemas map[string]*observer.Schema) ([]ScannedEndpoint, error) {
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

	// Find class-level @RequestMapping prefix.
	classPrefix := ""
	classRequiresAuth := false
	for _, line := range lines {
		if m := reSpringRequestMapping.FindStringSubmatch(line); m != nil {
			classPrefix = m[1]
		}
		t := strings.TrimSpace(line)
		if reSpringPreAuthorize.MatchString(t) || reSpringSecured.MatchString(t) || reSpringRolesAllowed.MatchString(t) {
			classRequiresAuth = true
		}
	}

	var endpoints []ScannedEndpoint
	var pendingMethod string
	var pendingMethodPath string
	var pendingDeprecated bool
	var pendingDescription string
	var pendingRequiresAuth bool

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		if reJavaDeprecated.MatchString(trimmed) {
			pendingDeprecated = true
			continue
		}

		if reSpringPreAuthorize.MatchString(trimmed) || reSpringSecured.MatchString(trimmed) || reSpringRolesAllowed.MatchString(trimmed) {
			pendingRequiresAuth = true
			continue
		}

		if m := reJavadoc.FindStringSubmatch(trimmed); m != nil {
			desc := strings.TrimSuffix(strings.TrimSpace(m[1]), "*/")
			if desc != "" && pendingDescription == "" {
				pendingDescription = desc
			}
			continue
		}

		if m := reSpringMethodMapping.FindStringSubmatch(trimmed); m != nil {
			pendingMethod = strings.ToUpper(m[1])
			pendingMethodPath = m[2]
			continue
		}

		if pendingMethod != "" {
			if m := reSpringActionMethod.FindStringSubmatch(trimmed); m != nil {
				responseType := m[1]
				fullPath := NormalizeFrameworkPath(classPrefix + "/" + strings.TrimLeft(pendingMethodPath, "/"))
				for strings.Contains(fullPath, "//") {
					fullPath = strings.ReplaceAll(fullPath, "//", "/")
				}
				if fullPath == "" {
					fullPath = "/"
				}

				ep := ScannedEndpoint{
					Method:       pendingMethod,
					PathPattern:  fullPath,
					Protocol:     "rest",
					Framework:    "spring",
					SourceFile:   absPath,
					SourceLine:   i + 1,
					Deprecated:   pendingDeprecated,
					Description:  pendingDescription,
					RequiresAuth: classRequiresAuth || pendingRequiresAuth,
				}

				if responseType != "" && responseType != "Void" && responseType != "void" {
					if s, ok := schemas[responseType]; ok {
						ep.RespSchema = s
					}
				}

				// Look for @RequestBody in method signature (same line).
				if m2 := reSpringRequestBody.FindStringSubmatch(trimmed); m2 != nil {
					if s, ok := schemas[m2[1]]; ok {
						ep.ReqSchema = s
					}
				}

				endpoints = append(endpoints, ep)
				pendingMethod = ""
				pendingMethodPath = ""
				pendingDeprecated = false
				pendingDescription = ""
				pendingRequiresAuth = false
			}
		}
	}
	return endpoints, nil
}

// extractJavaTypeSchemas collects Java class field schemas.
func extractJavaTypeSchemas(path string) (map[string]*observer.Schema, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	result := make(map[string]*observer.Schema)
	var currentClass string
	var currentSchema *observer.Schema
	var pendingNotNull bool
	var pendingEmail bool
	var pendingMinLen, pendingMaxLen int

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)

		if m := reJavaClassDecl.FindStringSubmatch(trimmed); m != nil {
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

		if reJavaNotNull.MatchString(trimmed) {
			pendingNotNull = true
		}
		if reJavaEmailAnnotation.MatchString(trimmed) {
			pendingEmail = true
		}
		if m := reJavaSize.FindStringSubmatch(trimmed); m != nil {
			pendingMinLen = atoi(m[1])
			pendingMaxLen = atoi(m[2])
		}

		if m := reJavaField.FindStringSubmatch(line); m != nil {
			javaType := m[1]
			fieldName := m[2]
			s := javaTypeToSchema(javaType)
			if pendingEmail {
				s.Format = "email"
			}
			if pendingMinLen > 0 {
				s.MinLength = pendingMinLen
			}
			if pendingMaxLen > 0 {
				s.MaxLength = pendingMaxLen
			}
			currentSchema.Properties[fieldName] = &s
			if pendingNotNull {
				currentSchema.Required = append(currentSchema.Required, fieldName)
			}
			pendingNotNull = false
			pendingEmail = false
			pendingMinLen = 0
			pendingMaxLen = 0
		}
	}
	if currentClass != "" && currentSchema != nil {
		result[currentClass] = currentSchema
	}
	return result, sc.Err()
}

// javaTypeToSchema converts a Java type name to observer.Schema.
func javaTypeToSchema(javaType string) observer.Schema {
	switch javaType {
	case "String":
		return observer.Schema{Type: "string"}
	case "Integer", "int", "Long", "long", "Short", "short", "Byte", "byte":
		return observer.Schema{Type: "integer"}
	case "Double", "double", "Float", "float", "BigDecimal":
		return observer.Schema{Type: "number"}
	case "Boolean", "bool":
		return observer.Schema{Type: "boolean"}
	default:
		if strings.HasPrefix(javaType, "List<") ||
			strings.HasPrefix(javaType, "Set<") ||
			strings.HasSuffix(javaType, "[]") {
			return observer.Schema{Type: "array"}
		}
		return observer.Schema{Type: "object"}
	}
}

// atoi converts a string to int, returning 0 on error.
func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// reJavaClassDecl matches Java class or record declarations.
var reJavaClassDecl = regexp.MustCompile(`(?:public\s+)?(?:class|record)\s+(\w+)`)

// walkJavaFiles walks dir recursively, calling fn for every .java file.
func walkJavaFiles(dir string, fn func(string) error) error {
	return walkWithSkip(dir, map[string]bool{
		"target":       true,
		"build":        true,
		".git":         true,
		"node_modules": true,
	}, ".java", fn)
}
