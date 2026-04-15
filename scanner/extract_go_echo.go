package scanner

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/AgusRdz/probe/config"
)

// goEchoExtractor implements Extractor for Echo router applications.
type goEchoExtractor struct{}

func (e *goEchoExtractor) Name() string { return "go-echo" }

// Detect returns true if go.mod contains labstack/echo.
func (e *goEchoExtractor) Detect(dir string) bool {
	return goModContains(dir, "labstack/echo")
}

// Echo route: e.GET("/path", handler) or group g.GET("/sub", handler)
var reEchoRoute = regexp.MustCompile(
	`(?i)\b(?:e|g|\w+)\.(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\s*\(\s*"([^"]+)"`,
)

// Echo group prefix: e.Group("/prefix")
var reEchoGroup = regexp.MustCompile(`\.Group\s*\(\s*"([^"]+)"`)

// Echo bind: c.Bind(&req)
var reEchoBind = regexp.MustCompile(`c\.Bind\s*\(\s*&(\w+)`)

// Extract walks dir and returns all discovered Echo endpoints.
func (e *goEchoExtractor) Extract(dir string, cfg *config.ScanConfig) ([]ScannedEndpoint, error) {
	var endpoints []ScannedEndpoint
	err := walkGoFiles(dir, func(path string) error {
		found, ferr := extractGoEchoFile(path)
		if ferr != nil {
			fmt.Fprintf(errorWriter, "scanner/go-echo: error reading %s: %v\n", path, ferr)
			return nil
		}
		endpoints = append(endpoints, found...)
		return nil
	})
	return endpoints, err
}

// extractGoEchoFile parses a Go source file for Echo routes.
func extractGoEchoFile(path string) ([]ScannedEndpoint, error) {
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

	prefix := ""
	for _, line := range lines {
		if m := reEchoGroup.FindStringSubmatch(line); m != nil {
			prefix = m[1]
			break
		}
	}

	var endpoints []ScannedEndpoint
	for i, line := range lines {
		m := reEchoRoute.FindStringSubmatch(line)
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
			Framework:   "go-echo",
			SourceFile:  absPath,
			SourceLine:  i + 1,
		}

		handlerName := extractHandlerNameFromLine(line)
		if handlerName != "" && src != nil {
			ep.ReqSchema = extractGoHandlerSchema(src, handlerName,
				[]string{"c.Bind"})
		}

		endpoints = append(endpoints, ep)
	}
	return endpoints, nil
}

// reEchoPathParam matches Echo :param syntax.
var reEchoPathParam = regexp.MustCompile(`:([A-Za-z_][A-Za-z0-9_]*)`)

// normalizeEchoPath converts Echo path params to {param}.
func normalizeEchoPath(path string) string {
	path = reEchoPathParam.ReplaceAllString(path, `{$1}`)
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	return path
}
