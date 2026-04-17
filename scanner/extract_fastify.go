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

// fastifyExtractor implements Extractor for Fastify applications.
type fastifyExtractor struct{}

func (e *fastifyExtractor) Name() string { return "fastify" }

// Detect returns true if the directory contains a package.json with fastify as a dependency.
func (e *fastifyExtractor) Detect(dir string) bool {
	pkgPath := filepath.Join(dir, "package.json")
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), `"fastify"`)
}

// Fastify method route: anyVar.get('/path', ...), anyVar.post('/path', ...), etc.
var reFastifyRoute = regexp.MustCompile(
	`(?i)\b\w+\.(get|post|put|patch|delete|head|options)\s*\(\s*['"]([^'"]+)['"]`,
)

// Fastify fastify.route({ method: 'GET', url: '/path', ... }) — matched per-line.
var reFastifyRouteMethod = regexp.MustCompile(`(?i)method\s*:\s*['"]([A-Z]+)['"]`)
var reFastifyRouteURL = regexp.MustCompile(`(?i)url\s*:\s*['"]([^'"]+)['"]`)

// Fastify register prefix: fastify.register(plugin, { prefix: '/api' })
var reFastifyRegister = regexp.MustCompile(`(?i)\w+\.register\s*\([^,]+,\s*\{[^}]*prefix\s*:\s*['"]([^'"]+)['"]`)

// Fastify inline body schema: schema: { body: { type: 'object', properties: { field: { type: 'string' } } } }
var reFastifyBodySchema = regexp.MustCompile(`(?i)schema\s*:\s*\{[^}]*body\s*:\s*\{[^}]*properties\s*:\s*\{([^}]+)\}`)
var reFastifySchemaField = regexp.MustCompile(`['"]?(\w+)['"]?\s*:\s*\{[^}]*type\s*:\s*['"](\w+)['"]`)

// skipFastifyDirs are sub-directories to skip during Fastify scanning.
var skipFastifyDirs = map[string]bool{
	"node_modules": true,
	"dist":         true,
	".next":        true,
	"coverage":     true,
	"vendor":       true,
	".git":         true,
}

// Extract walks dir and returns all discovered Fastify endpoints.
func (e *fastifyExtractor) Extract(dir string, cfg *config.ScanConfig) ([]ScannedEndpoint, error) {
	var endpoints []ScannedEndpoint
	err := walkJS(dir, skipFastifyDirs, func(path string) error {
		found, err := extractFastifyFile(path)
		if err != nil {
			fmt.Fprintf(errorWriter, "scanner/fastify: error reading %s: %v\n", path, err)
			return nil
		}
		endpoints = append(endpoints, found...)
		return nil
	})
	return endpoints, err
}

// extractFastifyFile parses a single JS/TS file for Fastify routes.
func extractFastifyFile(path string) ([]ScannedEndpoint, error) {
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

	jsdocBlocks := parseJSDocBlocks(lines)

	var endpoints []ScannedEndpoint
	absPath, _ := filepath.Abs(path)

	// Track last seen register prefix — simple single-prefix tracking.
	currentPrefix := ""

	for i, line := range lines {
		lineNo := i + 1

		// Track register prefix.
		if rm := reFastifyRegister.FindStringSubmatch(line); rm != nil {
			currentPrefix = rm[1]
		}

		// Method shorthand: fastify.get('/path', ...).
		if m := reFastifyRoute.FindStringSubmatch(line); m != nil {
			method := strings.ToUpper(m[1])
			rawPath := currentPrefix + m[2]
			ep := ScannedEndpoint{
				Method:      method,
				PathPattern: NormalizeFrameworkPath(rawPath),
				Protocol:    "rest",
				Framework:   "fastify",
				SourceFile:  absPath,
				SourceLine:  lineNo,
			}
			if block, ok := jsdocBlocks[i]; ok {
				ep.Description = block.description
				ep.Deprecated = block.deprecated
			}
			ep.ReqSchema = extractFastifyBodySchema(lines, i)
			endpoints = append(endpoints, ep)
			continue
		}

		// fastify.route({ method: '...', url: '...' }) — look for method and url on same line.
		if methodM := reFastifyRouteMethod.FindStringSubmatch(line); methodM != nil {
			method := strings.ToUpper(methodM[1])
			urlM := reFastifyRouteURL.FindStringSubmatch(line)
			if urlM != nil {
				rawPath := currentPrefix + urlM[1]
				ep := ScannedEndpoint{
					Method:      method,
					PathPattern: NormalizeFrameworkPath(rawPath),
					Protocol:    "rest",
					Framework:   "fastify",
					SourceFile:  absPath,
					SourceLine:  lineNo,
				}
				if block, ok := jsdocBlocks[i]; ok {
					ep.Description = block.description
					ep.Deprecated = block.deprecated
				}
				ep.ReqSchema = extractFastifyBodySchema(lines, i)
				endpoints = append(endpoints, ep)
			}
		}
	}

	return endpoints, nil
}

// extractFastifyBodySchema looks for an inline body schema in the lines around a route (up to 20 lines ahead).
func extractFastifyBodySchema(lines []string, routeIdx int) *observer.Schema {
	end := routeIdx + 20
	if end > len(lines) {
		end = len(lines)
	}
	for i := routeIdx; i < end; i++ {
		if bm := reFastifyBodySchema.FindStringSubmatch(lines[i]); bm != nil {
			schema := parseFastifyProperties(bm[1])
			if schema != nil {
				return schema
			}
		}
	}
	return nil
}

// parseFastifyProperties converts a properties block string into an observer.Schema.
func parseFastifyProperties(interior string) *observer.Schema {
	matches := reFastifySchemaField.FindAllStringSubmatch(interior, -1)
	if len(matches) == 0 {
		return nil
	}
	schema := &observer.Schema{
		Type:       "object",
		Properties: make(map[string]*observer.Schema),
	}
	for _, m := range matches {
		fieldName := m[1]
		fieldType := strings.ToLower(m[2])
		schema.Properties[fieldName] = zodTypeToSchema(fieldType)
	}
	return schema
}
