package scanner

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/AgusRdz/probe/config"
	"github.com/AgusRdz/probe/observer"
)

// goChiExtractor implements Extractor for Chi router applications.
type goChiExtractor struct{}

func (e *goChiExtractor) Name() string { return "go-chi" }

// Detect returns true if go.mod contains go-chi/chi.
func (e *goChiExtractor) Detect(dir string) bool {
	return goModContains(dir, "go-chi/chi")
}

// Chi route: r.Get("/path", handler) or r.Post("/path", handler)
var reChiRoute = regexp.MustCompile(
	`(?i)\br\.(Get|Post|Put|Patch|Delete|Head|Options|Connect|Trace)\s*\(\s*"([^"]+)"`,
)

// Chi group/mount prefix: r.Route("/prefix", ...) or r.Mount("/prefix", ...)
var reChiPrefix = regexp.MustCompile(
	`(?i)\br\.(Route|Mount)\s*\(\s*"([^"]+)"`,
)

// Extract walks dir and returns all discovered Chi endpoints.
func (e *goChiExtractor) Extract(dir string, cfg *config.ScanConfig) ([]ScannedEndpoint, error) {
	var endpoints []ScannedEndpoint
	err := walkGoFiles(dir, func(path string) error {
		found, ferr := extractGoChiFile(path)
		if ferr != nil {
			fmt.Fprintf(errorWriter, "scanner/go-chi: error reading %s: %v\n", path, ferr)
			return nil
		}
		endpoints = append(endpoints, found...)
		return nil
	})
	return endpoints, err
}

// extractGoChiFile parses a Go source file for Chi routes using regex.
func extractGoChiFile(path string) ([]ScannedEndpoint, error) {
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

	// Read full source for AST-based handler extraction.
	src, _ := os.ReadFile(path)

	// Simple prefix stack — one level only (handles most real-world cases).
	prefix := ""
	for _, line := range lines {
		if m := reChiPrefix.FindStringSubmatch(line); m != nil {
			prefix = m[2]
			break
		}
	}

	var endpoints []ScannedEndpoint
	for i, line := range lines {
		m := reChiRoute.FindStringSubmatch(line)
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
			Framework:   "go-chi",
			SourceFile:  absPath,
			SourceLine:  i + 1,
		}

		// Try to find the handler name and extract its request body type.
		handlerName := extractHandlerNameFromLine(line)
		if handlerName != "" && src != nil {
			ep.ReqSchema = extractGoHandlerSchema(src, handlerName,
				[]string{"json.NewDecoder", "json.Decode", "json.Unmarshal"})
		}

		endpoints = append(endpoints, ep)
	}
	return endpoints, nil
}

// reHandlerName matches the last identifier argument in a route call.
var reHandlerName = regexp.MustCompile(`,\s*(\w+)\s*\)`)

// extractHandlerNameFromLine extracts the last identifier argument in a route call.
// e.g. r.Get("/path", myHandler) → "myHandler"
func extractHandlerNameFromLine(line string) string {
	m := reHandlerName.FindStringSubmatch(line)
	if m != nil {
		return m[1]
	}
	return ""
}

// extractGoHandlerSchema uses go/ast to find the request body struct type used in
// the named handler function. decodePatterns are substrings indicating JSON decode.
func extractGoHandlerSchema(src []byte, handlerName string, decodePatterns []string) *observer.Schema {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, 0)
	if err != nil {
		return nil
	}

	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name.Name != handlerName || fd.Body == nil {
			continue
		}

		// Walk the function body looking for var decls and decode calls.
		typeName := ""
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			if typeName != "" {
				return false
			}
			switch v := n.(type) {
			case *ast.DeclStmt:
				// var req CreateUserRequest
				if gd, ok := v.Decl.(*ast.GenDecl); ok {
					for _, spec := range gd.Specs {
						if vs, ok := spec.(*ast.ValueSpec); ok {
							if id, ok := vs.Type.(*ast.Ident); ok {
								typeName = id.Name
							}
						}
					}
				}
			case *ast.AssignStmt:
				// req := CreateUserRequest{} or var req CreateUserRequest
				for _, rhs := range v.Rhs {
					if cl, ok := rhs.(*ast.CompositeLit); ok {
						if id, ok := cl.Type.(*ast.Ident); ok {
							typeName = id.Name
						}
					}
				}
			}
			return typeName == ""
		})

		if typeName != "" {
			return extractGoStructSchema(src, typeName)
		}
	}
	return nil
}

// goModContains checks if go.mod in dir contains the given module path.
func goModContains(dir, module string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), module)
}
