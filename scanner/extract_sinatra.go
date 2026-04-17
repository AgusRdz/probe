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

// sinatraExtractor implements Extractor for Sinatra Ruby applications.
type sinatraExtractor struct{}

func (e *sinatraExtractor) Name() string { return "sinatra" }

// Detect returns true if Gemfile in dir mentions sinatra.
func (e *sinatraExtractor) Detect(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "Gemfile"))
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), "sinatra")
}

// get '/path' do  (or post, put, patch, delete, options)
var reSinatraVerb = regexp.MustCompile(`^\s*(get|post|put|patch|delete|options)\s+['"]([^'"]+)['"]\s+do`)

// :id → {id}
var reSinatraPathParam = regexp.MustCompile(`:(\w+)`)

// # @deprecated
var reSinatraDeprecated = regexp.MustCompile(`#\s*@deprecated`)

// # Some description (non-@deprecated comment)
var reSinatraDescription = regexp.MustCompile(`#\s*(.+)`)

// Extract walks .rb files and extracts Sinatra route definitions.
func (e *sinatraExtractor) Extract(dir string, cfg *config.ScanConfig) ([]ScannedEndpoint, error) {
	var endpoints []ScannedEndpoint
	skip := map[string]bool{
		"vendor": true,
		".git":   true,
		"spec":   true,
		"test":   true,
	}
	err := walkWithSkip(dir, skip, ".rb", func(path string) error {
		found, ferr := extractSinatraFile(path)
		if ferr != nil {
			fmt.Fprintf(errorWriter, "scanner/sinatra: error reading %s: %v\n", path, ferr)
			return nil
		}
		endpoints = append(endpoints, found...)
		return nil
	})
	return endpoints, err
}

// extractSinatraFile parses a single .rb file for Sinatra route definitions.
func extractSinatraFile(path string) ([]ScannedEndpoint, error) {
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

	for i, line := range lines {
		m := reSinatraVerb.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		method := strings.ToUpper(m[1])
		rawPath := m[2]

		ep := ScannedEndpoint{
			Method:      method,
			PathPattern: normalizeSinatraPath(rawPath),
			Protocol:    "rest",
			Framework:   "sinatra",
			SourceFile:  absPath,
			SourceLine:  i + 1,
		}

		// Scan preceding lines for description / deprecated comment.
		for j := i - 1; j >= 0 && j >= i-3; j-- {
			prev := strings.TrimSpace(lines[j])
			if prev == "" {
				break
			}
			if reSinatraDeprecated.MatchString(prev) {
				ep.Deprecated = true
				break
			}
			if dm := reSinatraDescription.FindStringSubmatch(prev); dm != nil {
				text := strings.TrimSpace(dm[1])
				if !strings.HasPrefix(text, "@") && ep.Description == "" {
					ep.Description = text
				}
			}
		}

		endpoints = append(endpoints, ep)
	}

	return endpoints, nil
}

// normalizeSinatraPath converts :param to {param} and cleans double slashes.
func normalizeSinatraPath(path string) string {
	path = reSinatraPathParam.ReplaceAllString(path, `{$1}`)
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}
