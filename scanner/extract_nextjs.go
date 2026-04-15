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

// nextjsExtractor implements Extractor for Next.js applications.
type nextjsExtractor struct{}

func (n *nextjsExtractor) Name() string { return "nextjs" }

// Detect returns true if the directory contains a package.json with "next".
func (n *nextjsExtractor) Detect(dir string) bool {
	pkgPath := filepath.Join(dir, "package.json")
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), `"next"`)
}

// Matches Next.js app router export: export async function GET(...) or export function GET(...)
var reAppRouterExport = regexp.MustCompile(`(?m)export\s+(?:async\s+)?function\s+(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\s*\(`)

// Matches Next.js pages router export default (infers GET+POST).
var rePagesDefaultExport = regexp.MustCompile(`export\s+default\s+(?:async\s+)?function`)

// skipNextDirs are directories skipped during Next.js scanning.
var skipNextDirs = map[string]bool{
	"node_modules": true,
	"dist":         true,
	".next":        true,
	"coverage":     true,
	"vendor":       true,
	".git":         true,
	"__pycache__":  true,
}

// Extract walks dir and returns all discovered Next.js endpoints.
func (n *nextjsExtractor) Extract(dir string, cfg *config.ScanConfig) ([]ScannedEndpoint, error) {
	var endpoints []ScannedEndpoint

	// Walk pages/api and app/api directories.
	pagesAPIDir := filepath.Join(dir, "pages", "api")
	appAPIDir := filepath.Join(dir, "app", "api")

	if _, err := os.Stat(pagesAPIDir); err == nil {
		found, err := extractNextJSPagesRouter(pagesAPIDir, pagesAPIDir)
		if err != nil {
			fmt.Fprintf(errorWriter, "scanner/nextjs: pages/api error: %v\n", err)
		} else {
			endpoints = append(endpoints, found...)
		}
	}

	if _, err := os.Stat(appAPIDir); err == nil {
		found, err := extractNextJSAppRouter(appAPIDir, appAPIDir)
		if err != nil {
			fmt.Fprintf(errorWriter, "scanner/nextjs: app/api error: %v\n", err)
		} else {
			endpoints = append(endpoints, found...)
		}
	}

	// Also support src/pages/api and src/app/api.
	srcPagesAPIDir := filepath.Join(dir, "src", "pages", "api")
	srcAppAPIDir := filepath.Join(dir, "src", "app", "api")

	if _, err := os.Stat(srcPagesAPIDir); err == nil {
		found, err := extractNextJSPagesRouter(srcPagesAPIDir, srcPagesAPIDir)
		if err != nil {
			fmt.Fprintf(errorWriter, "scanner/nextjs: src/pages/api error: %v\n", err)
		} else {
			endpoints = append(endpoints, found...)
		}
	}

	if _, err := os.Stat(srcAppAPIDir); err == nil {
		found, err := extractNextJSAppRouter(srcAppAPIDir, srcAppAPIDir)
		if err != nil {
			fmt.Fprintf(errorWriter, "scanner/nextjs: src/app/api error: %v\n", err)
		} else {
			endpoints = append(endpoints, found...)
		}
	}

	return endpoints, nil
}

// extractNextJSPagesRouter walks the pages/api directory and infers routes from file paths.
// pages/api/users/[id].ts → GET+POST /api/users/{id}
func extractNextJSPagesRouter(baseDir, dir string) ([]ScannedEndpoint, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var endpoints []ScannedEndpoint
	for _, e := range entries {
		name := e.Name()
		fullPath := filepath.Join(dir, name)

		if e.IsDir() {
			if skipNextDirs[name] {
				continue
			}
			found, err := extractNextJSPagesRouter(baseDir, fullPath)
			if err != nil {
				fmt.Fprintf(errorWriter, "scanner/nextjs: error in %s: %v\n", fullPath, err)
				continue
			}
			endpoints = append(endpoints, found...)
			continue
		}

		ext := filepath.Ext(name)
		if ext != ".ts" && ext != ".js" && ext != ".tsx" && ext != ".jsx" {
			continue
		}

		// Compute the URL path from the file path relative to pages/api.
		rel, err := filepath.Rel(baseDir, fullPath)
		if err != nil {
			continue
		}

		urlPath := pagesFileToURLPath(rel)
		if urlPath == "" {
			continue
		}

		absPath, _ := filepath.Abs(fullPath)

		// Infer methods from file content.
		methods := inferPagesRouterMethods(fullPath)

		for _, method := range methods {
			endpoints = append(endpoints, ScannedEndpoint{
				Method:      method,
				PathPattern: "/api/" + strings.TrimPrefix(urlPath, "/"),
				Protocol:    "rest",
				Framework:   "nextjs",
				SourceFile:  absPath,
				SourceLine:  1,
			})
		}
	}
	return endpoints, nil
}

// pagesFileToURLPath converts a relative file path (from pages/api base) to a URL path segment.
// e.g. users/[id].ts → users/{id}
//
//	index.ts → ""  (maps to empty, parent is used)
func pagesFileToURLPath(rel string) string {
	// Normalize separators.
	rel = filepath.ToSlash(rel)

	// Remove extension.
	for _, ext := range []string{".ts", ".js", ".tsx", ".jsx"} {
		if strings.HasSuffix(rel, ext) {
			rel = rel[:len(rel)-len(ext)]
			break
		}
	}

	// index → parent directory route.
	if rel == "index" {
		return ""
	}

	// Split path parts and normalize each segment.
	parts := strings.Split(rel, "/")
	var normalized []string
	for _, p := range parts {
		if p == "index" {
			// index at non-root level maps to parent path.
			continue
		}
		normalized = append(normalized, nextjsSegmentToParam(p))
	}

	return strings.Join(normalized, "/")
}

// nextjsSegmentToParam converts a Next.js dynamic segment to {param} format.
// [id] → {id}
// [...slug] → {slug}
func nextjsSegmentToParam(segment string) string {
	// Catch-all: [...slug]
	if strings.HasPrefix(segment, "[...") && strings.HasSuffix(segment, "]") {
		inner := segment[4 : len(segment)-1]
		return "{" + inner + "}"
	}
	// Dynamic: [id]
	if strings.HasPrefix(segment, "[") && strings.HasSuffix(segment, "]") {
		inner := segment[1 : len(segment)-1]
		return "{" + inner + "}"
	}
	return segment
}

// inferPagesRouterMethods reads a pages/api file and infers which HTTP methods it handles.
// Default export → GET + POST (typical handler pattern).
func inferPagesRouterMethods(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return []string{"GET", "POST"}
	}
	defer f.Close()

	content := readFileContent(f)

	// Check for method-gating patterns: req.method === 'GET'
	methods := extractMethodConditions(content)
	if len(methods) > 0 {
		return methods
	}

	// Default export present → assume GET + POST.
	if rePagesDefaultExport.MatchString(content) {
		return []string{"GET", "POST"}
	}

	return []string{"GET", "POST"}
}

// reMethodCondition matches patterns like req.method === 'GET' or req.method == "POST"
var reMethodCondition = regexp.MustCompile(`req\.method\s*===?\s*['"]([A-Z]+)['"]`)

// extractMethodConditions extracts HTTP methods from method-guard conditions.
func extractMethodConditions(content string) []string {
	matches := reMethodCondition.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	var result []string
	for _, m := range matches {
		if !seen[m[1]] {
			seen[m[1]] = true
			result = append(result, m[1])
		}
	}
	return result
}

// extractNextJSAppRouter walks the app/api directory and infers routes from file paths
// and exported function names.
// app/api/users/[id]/route.ts with export GET → GET /api/users/{id}
func extractNextJSAppRouter(baseDir, dir string) ([]ScannedEndpoint, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var endpoints []ScannedEndpoint
	for _, e := range entries {
		name := e.Name()
		fullPath := filepath.Join(dir, name)

		if e.IsDir() {
			if skipNextDirs[name] {
				continue
			}
			found, err := extractNextJSAppRouter(baseDir, fullPath)
			if err != nil {
				fmt.Fprintf(errorWriter, "scanner/nextjs: error in %s: %v\n", fullPath, err)
				continue
			}
			endpoints = append(endpoints, found...)
			continue
		}

		// Only process route.ts / route.js / route.tsx / route.jsx files.
		base := strings.ToLower(name)
		isRoute := base == "route.ts" || base == "route.js" || base == "route.tsx" || base == "route.jsx"
		if !isRoute {
			continue
		}

		// Compute URL path from directory path relative to app/api base.
		relDir, err := filepath.Rel(baseDir, dir)
		if err != nil {
			continue
		}

		urlPath := appDirToURLPath(relDir)
		absPath, _ := filepath.Abs(fullPath)

		// Read file to find exported HTTP methods.
		methods, lineNos := inferAppRouterMethods(fullPath)

		for idx, method := range methods {
			lineNo := 1
			if idx < len(lineNos) {
				lineNo = lineNos[idx]
			}
			ep := ScannedEndpoint{
				Method:      method,
				PathPattern: "/api/" + strings.TrimPrefix(urlPath, "/"),
				Protocol:    "rest",
				Framework:   "nextjs",
				SourceFile:  absPath,
				SourceLine:  lineNo,
			}
			endpoints = append(endpoints, ep)
		}
	}
	return endpoints, nil
}

// appDirToURLPath converts a relative directory path (from app/api base) to a URL path.
// "." → ""
// "users/[id]" → "users/{id}"
func appDirToURLPath(relDir string) string {
	relDir = filepath.ToSlash(relDir)
	if relDir == "." || relDir == "" {
		return ""
	}
	parts := strings.Split(relDir, "/")
	var normalized []string
	for _, p := range parts {
		normalized = append(normalized, nextjsSegmentToParam(p))
	}
	return strings.Join(normalized, "/")
}

// inferAppRouterMethods reads a route.ts file and returns the exported HTTP methods
// along with their line numbers.
func inferAppRouterMethods(path string) (methods []string, lineNos []int) {
	f, err := os.Open(path)
	if err != nil {
		return []string{"GET"}, []int{1}
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	lineNo := 0
	httpMethods := []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}
	seen := make(map[string]bool)

	for sc.Scan() {
		lineNo++
		line := sc.Text()
		for _, m := range httpMethods {
			if seen[m] {
				continue
			}
			// Match: export async function GET / export function GET
			pat := regexp.MustCompile(`export\s+(?:async\s+)?function\s+` + m + `\s*\(`)
			if pat.MatchString(line) {
				methods = append(methods, m)
				lineNos = append(lineNos, lineNo)
				seen[m] = true
			}
		}
	}
	return methods, lineNos
}

// readFileContent reads all content from an open file.
func readFileContent(f *os.File) string {
	sc := bufio.NewScanner(f)
	var sb strings.Builder
	for sc.Scan() {
		sb.WriteString(sc.Text())
		sb.WriteByte('\n')
	}
	return sb.String()
}
