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

// goFiberExtractor implements Extractor for Fiber router applications.
type goFiberExtractor struct{}

func (e *goFiberExtractor) Name() string { return "go-fiber" }

// Detect returns true if go.mod contains gofiber/fiber.
func (e *goFiberExtractor) Detect(dir string) bool {
	return goModContains(dir, "gofiber/fiber")
}

// Fiber route: app.Get("/path", handler) or app.Post("/path", handler)
var reFiberRoute = regexp.MustCompile(
	`(?i)\bapp\.(Get|Post|Put|Patch|Delete|Head|Options|All)\s*\(\s*"([^"]+)"`,
)

// Fiber group prefix: app.Group("/prefix")
var reFiberGroup = regexp.MustCompile(`\.Group\s*\(\s*"([^"]+)"`)

// Fiber body parser: c.BodyParser(&req)
var reFiberBodyParser = regexp.MustCompile(`c\.BodyParser\s*\(\s*&(\w+)`)

// Extract walks dir and returns all discovered Fiber endpoints.
func (e *goFiberExtractor) Extract(dir string, cfg *config.ScanConfig) ([]ScannedEndpoint, error) {
	var endpoints []ScannedEndpoint
	err := walkGoFiles(dir, func(path string) error {
		found, ferr := extractGoFiberFile(path)
		if ferr != nil {
			fmt.Fprintf(errorWriter, "scanner/go-fiber: error reading %s: %v\n", path, ferr)
			return nil
		}
		endpoints = append(endpoints, found...)
		return nil
	})
	return endpoints, err
}

// extractGoFiberFile parses a Go source file for Fiber routes.
func extractGoFiberFile(path string) ([]ScannedEndpoint, error) {
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
		if m := reFiberGroup.FindStringSubmatch(line); m != nil {
			prefix = m[1]
			break
		}
	}

	var endpoints []ScannedEndpoint
	for i, line := range lines {
		m := reFiberRoute.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		method := strings.ToUpper(m[1])
		rawPath := m[2]
		// Fiber uses :param style (same as Express).
		fullPath := NormalizeFrameworkPath(prefix + rawPath)

		ep := ScannedEndpoint{
			Method:      method,
			PathPattern: fullPath,
			Protocol:    "rest",
			Framework:   "go-fiber",
			SourceFile:  absPath,
			SourceLine:  i + 1,
		}

		handlerName := extractHandlerNameFromLine(line)
		if handlerName != "" && src != nil {
			ep.ReqSchema = extractGoHandlerSchema(src, handlerName,
				[]string{"BodyParser"})
		}

		endpoints = append(endpoints, ep)
	}
	return endpoints, nil
}

