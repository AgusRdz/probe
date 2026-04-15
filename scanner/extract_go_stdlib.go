package scanner

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/AgusRdz/probe/config"
)

// goStdlibExtractor implements Extractor for Go net/http standard library applications.
type goStdlibExtractor struct{}

func (e *goStdlibExtractor) Name() string { return "go-stdlib" }

// Detect returns true if go.mod exists but no known web framework is present.
func (e *goStdlibExtractor) Detect(dir string) bool {
	if !goModExists(dir) {
		return false
	}
	// Suppress if a known framework is already detected.
	if goModContains(dir, "go-chi/chi") ||
		goModContains(dir, "gin-gonic/gin") ||
		goModContains(dir, "labstack/echo") ||
		goModContains(dir, "gofiber/fiber") {
		return false
	}
	return true
}

// net/http HandleFunc and Handle patterns.
var reStdlibHandleFunc = regexp.MustCompile(
	`(?:mux|http)\.HandleFunc\s*\(\s*"([^"]+)"`,
)

var reStdlibHandle = regexp.MustCompile(
	`(?:mux|http)\.Handle\s*\(\s*"([^"]+)"`,
)

// Extract walks dir and returns all discovered stdlib HTTP endpoints.
func (e *goStdlibExtractor) Extract(dir string, cfg *config.ScanConfig) ([]ScannedEndpoint, error) {
	var endpoints []ScannedEndpoint
	err := walkGoFiles(dir, func(path string) error {
		found, ferr := extractGoStdlibFile(path)
		if ferr != nil {
			fmt.Fprintf(errorWriter, "scanner/go-stdlib: error reading %s: %v\n", path, ferr)
			return nil
		}
		endpoints = append(endpoints, found...)
		return nil
	})
	return endpoints, err
}

// extractGoStdlibFile parses a Go source file for net/http route registrations.
func extractGoStdlibFile(path string) ([]ScannedEndpoint, error) {
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

	var endpoints []ScannedEndpoint
	for i, line := range lines {
		var rawPath string
		matched := false

		if m := reStdlibHandleFunc.FindStringSubmatch(line); m != nil {
			rawPath = m[1]
			matched = true
		} else if m := reStdlibHandle.FindStringSubmatch(line); m != nil {
			rawPath = m[1]
			matched = true
		}

		if !matched {
			continue
		}

		fullPath := NormalizeFrameworkPath(rawPath)

		// net/http HandleFunc has no method — default to GET,POST.
		for _, method := range []string{"GET", "POST"} {
			ep := ScannedEndpoint{
				Method:      method,
				PathPattern: fullPath,
				Protocol:    "rest",
				Framework:   "go-stdlib",
				SourceFile:  absPath,
				SourceLine:  i + 1,
			}

			handlerName := extractHandlerNameFromLine(line)
			if handlerName != "" && src != nil {
				ep.ReqSchema = extractGoHandlerSchema(src, handlerName,
					[]string{"json.NewDecoder", "json.Decode", "json.Unmarshal"})
			}

			endpoints = append(endpoints, ep)
		}
	}
	return endpoints, nil
}

// goModExists returns true if a go.mod file is present in dir.
func goModExists(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "go.mod"))
	return err == nil
}

