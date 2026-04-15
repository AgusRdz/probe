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

// goGinExtractor implements Extractor for Gin router applications.
type goGinExtractor struct{}

func (e *goGinExtractor) Name() string { return "go-gin" }

// Detect returns true if go.mod contains gin-gonic/gin.
func (e *goGinExtractor) Detect(dir string) bool {
	return goModContains(dir, "gin-gonic/gin")
}

// Gin route: r.GET("/path", handler) or r.POST("/path", handler)
var reGinRoute = regexp.MustCompile(
	`(?i)\b\w+\.(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|Any)\s*\(\s*"([^"]+)"`,
)

// Gin group prefix: r.Group("/prefix")
var reGinGroup = regexp.MustCompile(`\.Group\s*\(\s*"([^"]+)"`)

// Gin bind: c.ShouldBindJSON(&req) or c.BindJSON(&req)
var reGinBind = regexp.MustCompile(`c\.(?:ShouldBindJSON|BindJSON)\s*\(\s*&(\w+)`)

// Gin path param: :param → {param}, *param → {param}
var reGinPathParam = regexp.MustCompile(`:([A-Za-z_][A-Za-z0-9_]*)`)
var reGinWildcard = regexp.MustCompile(`\*([A-Za-z_][A-Za-z0-9_]*)`)

// Extract walks dir and returns all discovered Gin endpoints.
func (e *goGinExtractor) Extract(dir string, cfg *config.ScanConfig) ([]ScannedEndpoint, error) {
	var endpoints []ScannedEndpoint
	err := walkGoFiles(dir, func(path string) error {
		found, ferr := extractGoGinFile(path)
		if ferr != nil {
			fmt.Fprintf(errorWriter, "scanner/go-gin: error reading %s: %v\n", path, ferr)
			return nil
		}
		endpoints = append(endpoints, found...)
		return nil
	})
	return endpoints, err
}

// extractGoGinFile parses a Go source file for Gin routes.
func extractGoGinFile(path string) ([]ScannedEndpoint, error) {
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
	src, _ := os.ReadFile(path)

	// Simple one-level group prefix detection.
	prefix := ""
	for _, line := range lines {
		if m := reGinGroup.FindStringSubmatch(line); m != nil {
			prefix = m[1]
			break
		}
	}

	var endpoints []ScannedEndpoint
	for i, line := range lines {
		m := reGinRoute.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		method := strings.ToUpper(m[1])
		rawPath := m[2]
		fullPath := normalizeGinPath(prefix + rawPath)

		ep := ScannedEndpoint{
			Method:      method,
			PathPattern: fullPath,
			Protocol:    "rest",
			Framework:   "go-gin",
			SourceFile:  absPath,
			SourceLine:  i + 1,
		}

		// Find handler name and resolve request schema.
		handlerName := extractHandlerNameFromLine(line)
		if handlerName != "" && src != nil {
			ep.ReqSchema = extractGoHandlerSchema(src, handlerName,
				[]string{"ShouldBindJSON", "BindJSON"})

			// Also scan handler body directly for bind calls.
			if ep.ReqSchema == nil {
				ep.ReqSchema = extractGinBindSchema(src, handlerName)
			}
		}

		endpoints = append(endpoints, ep)
	}
	return endpoints, nil
}

// normalizeGinPath converts Gin path params (:param, *param) to {param}.
func normalizeGinPath(path string) string {
	path = reGinPathParam.ReplaceAllString(path, `{$1}`)
	path = reGinWildcard.ReplaceAllString(path, `{$1}`)
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	return path
}

// extractGinBindSchema scans the source for c.ShouldBindJSON(&varName) and
// resolves varName's type via AST.
func extractGinBindSchema(src []byte, handlerName string) *observer.Schema {
	// Find bind call pattern in source text to get variable name.
	m := reGinBind.FindSubmatch(src)
	if m == nil {
		return nil
	}
	varName := string(m[1])
	if varName == "" {
		return nil
	}
	// Try to find the type of varName in the handler.
	return findVarTypeAndExtract(src, handlerName, varName)
}

// findVarTypeAndExtract finds the declared type of varName inside handlerName
// and extracts its schema.
func findVarTypeAndExtract(src []byte, _, varName string) *observer.Schema {
	// Quick regex approach: look for "var varName TypeName" or "varName TypeName"
	reVarDecl := regexp.MustCompile(`(?:var\s+` + regexp.QuoteMeta(varName) + `\s+(\w+)|` +
		regexp.QuoteMeta(varName) + `\s+(\w+)\s*[{=])`)
	m := reVarDecl.FindSubmatch(src)
	if m == nil {
		return nil
	}
	typeName := string(m[1])
	if typeName == "" {
		typeName = string(m[2])
	}
	if typeName == "" {
		return nil
	}
	return extractGoStructSchema(src, typeName)
}
