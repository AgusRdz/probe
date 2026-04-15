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

// axumExtractor implements Extractor for Axum router applications.
type axumExtractor struct{}

func (e *axumExtractor) Name() string { return "axum" }

// Detect returns true if Cargo.toml mentions axum.
func (e *axumExtractor) Detect(dir string) bool {
	return cargoTomlContains(dir, "axum")
}

// Router::new().route("/path", get(handler))
var reAxumRoute = regexp.MustCompile(
	`\.route\s*\(\s*"([^"]+)"\s*,\s*(get|post|put|patch|delete|head|options)\s*\(`,
)

// .route("/path", post(h1).put(h2)) — multi-method chained
var reAxumMultiRoute = regexp.MustCompile(
	`\.route\s*\(\s*"([^"]+)"\s*,\s*(.+)\)`,
)

// Router::new().nest("/prefix", sub_router)
var reAxumNest = regexp.MustCompile(`\.nest\s*\(\s*"([^"]+)"`)

// async fn handler(Json(body): Json<TypeName>) — extract TypeName
var reAxumJsonExtractor = regexp.MustCompile(`Json\s*<(\w+)>`)

// Path(id): Path<u32> — path param extraction
var reAxumPathParam = regexp.MustCompile(`Path\s*<(\w+)>`)

// HTTP method names in chained expression.
var reAxumMethodName = regexp.MustCompile(`\b(get|post|put|patch|delete|head|options)\b`)

// Extract walks dir and returns all discovered Axum endpoints.
func (e *axumExtractor) Extract(dir string, cfg *config.ScanConfig) ([]ScannedEndpoint, error) {
	// Collect Serde struct schemas (shared with actix extractor logic).
	schemas := make(map[string]*observer.Schema)
	_ = walkRustFiles(dir, func(path string) error {
		found, ferr := extractRustStructSchemas(path)
		if ferr != nil {
			return nil
		}
		for k, v := range found {
			schemas[k] = v
		}
		return nil
	})

	var endpoints []ScannedEndpoint
	err := walkRustFiles(dir, func(path string) error {
		found, ferr := extractAxumFile(path, schemas)
		if ferr != nil {
			fmt.Fprintf(errorWriter, "scanner/axum: error reading %s: %v\n", path, ferr)
			return nil
		}
		endpoints = append(endpoints, found...)
		return nil
	})
	return endpoints, err
}

// extractAxumFile parses a .rs file for Axum route definitions.
func extractAxumFile(path string, schemas map[string]*observer.Schema) ([]ScannedEndpoint, error) {
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

	// Detect nest prefix — one level.
	prefix := ""
	for _, line := range lines {
		if m := reAxumNest.FindStringSubmatch(line); m != nil {
			prefix = m[1]
			break
		}
	}

	var endpoints []ScannedEndpoint
	for i, line := range lines {
		m := reAxumRoute.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		rawPath := m[1]
		method := strings.ToUpper(m[2])
		fullPath := normalizeActixPath(prefix + rawPath)

		ep := ScannedEndpoint{
			Method:      method,
			PathPattern: fullPath,
			Protocol:    "rest",
			Framework:   "axum",
			SourceFile:  absPath,
			SourceLine:  i + 1,
		}

		// Extract handler name to look up body type.
		handlerRe := regexp.MustCompile(strings.ToLower(method) + `\s*\(\s*(\w+)\s*\)`)
		if hm := handlerRe.FindStringSubmatch(line); hm != nil {
			handlerName := hm[1]
			// Search all lines for the handler's Json extractor.
			ep.ReqSchema = findAxumHandlerSchema(lines, handlerName, schemas)
		}

		endpoints = append(endpoints, ep)
	}
	return endpoints, nil
}

// findAxumHandlerSchema searches for an async fn <handlerName> and extracts
// its Json<T> body type from the parameter list.
func findAxumHandlerSchema(lines []string, handlerName string, schemas map[string]*observer.Schema) *observer.Schema {
	reFn := regexp.MustCompile(`async\s+fn\s+` + regexp.QuoteMeta(handlerName) + `\s*\(`)
	for i, line := range lines {
		if !reFn.MatchString(line) {
			continue
		}
		// Scan function signature (may span a few lines).
		end := i + 5
		if end > len(lines) {
			end = len(lines)
		}
		for j := i; j < end; j++ {
			if m := reAxumJsonExtractor.FindStringSubmatch(lines[j]); m != nil {
				if s, ok := schemas[m[1]]; ok {
					return s
				}
			}
		}
	}
	return nil
}
