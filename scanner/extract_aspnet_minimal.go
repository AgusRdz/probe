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

// aspnetMinimalExtractor implements Extractor for ASP.NET Core Minimal API apps.
type aspnetMinimalExtractor struct{}

func (e *aspnetMinimalExtractor) Name() string { return "aspnet-minimal" }

// Detect returns true if a .csproj file exists (same detection as MVC; both run).
func (e *aspnetMinimalExtractor) Detect(dir string) bool {
	return csprojExists(dir)
}

// <varname>.MapGet("/path", handler) or app.MapPost("/path", ...)
var reMinimalMapRoute = regexp.MustCompile(
	`(\w+)\.Map(Get|Post|Put|Patch|Delete|Head|Options)\s*\(\s*"([^"]+)"`,
)

// var <varname> = <any>.MapGroup("/prefix")
var reMinimalMapGroup = regexp.MustCompile(
	`(\w+)\s*=\s*\w+\.MapGroup\s*\(\s*"([^"]+)"`,
)

// Lambda param: (TypeName paramName) or ([FromBody] TypeName paramName)
var reMinimalLambdaParam = regexp.MustCompile(
	`\(\s*(?:\[\w+\]\s+)?(\w+)\s+\w+\s*\)`,
)

// Results.Ok<TypeName>() response type extraction
var reMinimalResultsOk = regexp.MustCompile(`Results\.Ok<(\w+)>\s*\(`)

// Extract walks dir and returns all discovered Minimal API endpoints.
func (e *aspnetMinimalExtractor) Extract(dir string, cfg *config.ScanConfig) ([]ScannedEndpoint, error) {
	// Collect C# schemas shared with MVC extractor.
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
		found, ferr := extractASPNetMinimalFile(path, schemas)
		if ferr != nil {
			fmt.Fprintf(errorWriter, "scanner/aspnet-minimal: error reading %s: %v\n", path, ferr)
			return nil
		}
		endpoints = append(endpoints, found...)
		return nil
	})
	return endpoints, err
}

// extractASPNetMinimalFile parses a C# file for Minimal API route registrations.
func extractASPNetMinimalFile(path string, schemas map[string]*observer.Schema) ([]ScannedEndpoint, error) {
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

	// Build variable-name → group prefix map from MapGroup() calls.
	// e.g. var api = app.MapGroup("/api/v1") → groupPrefixes["api"] = "/api/v1"
	// "app" itself has no prefix.
	groupPrefixes := map[string]string{"app": ""}
	for _, line := range lines {
		if m := reMinimalMapGroup.FindStringSubmatch(line); m != nil {
			groupPrefixes[m[1]] = m[2]
		}
	}

	var endpoints []ScannedEndpoint
	for i, line := range lines {
		m := reMinimalMapRoute.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		receiverVar := m[1]
		method := strings.ToUpper(m[2])
		rawPath := m[3]
		prefix := groupPrefixes[receiverVar] // empty string if not a group variable
		fullPath := NormalizeFrameworkPath(prefix + rawPath)
		for strings.Contains(fullPath, "//") {
			fullPath = strings.ReplaceAll(fullPath, "//", "/")
		}

		ep := ScannedEndpoint{
			Method:      method,
			PathPattern: fullPath,
			Protocol:    "rest",
			Framework:   "aspnet-minimal",
			SourceFile:  absPath,
			SourceLine:  i + 1,
		}

		// Lambda param type extraction.
		if m2 := reMinimalLambdaParam.FindStringSubmatch(line); m2 != nil {
			typeName := m2[1]
			if s, ok := schemas[typeName]; ok {
				ep.ReqSchema = s
			}
		}

		// Response type from Results.Ok<T>.
		// Scan forward a few lines for the handler body.
		end := i + 15
		if end > len(lines) {
			end = len(lines)
		}
		for j := i; j < end; j++ {
			if m3 := reMinimalResultsOk.FindStringSubmatch(lines[j]); m3 != nil {
				if s, ok := schemas[m3[1]]; ok {
					ep.RespSchema = s
				}
				break
			}
		}

		endpoints = append(endpoints, ep)
	}
	return endpoints, nil
}
