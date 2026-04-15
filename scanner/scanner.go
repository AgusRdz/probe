package scanner

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/AgusRdz/probe/config"
)

// errorWriter is the destination for scanner error messages.
// Replaced in tests to suppress noise.
var errorWriter io.Writer = os.Stderr

// registry holds all registered Extractor implementations.
var registry []Extractor

func init() {
	registry = []Extractor{
		// JavaScript / TypeScript
		&expressExtractor{},
		&nestjsExtractor{},
		&nextjsExtractor{},
		// Python
		&fastAPIExtractor{},
		&flaskExtractor{},
		&djangoExtractor{},
		// Go
		&goChiExtractor{},
		&goGinExtractor{},
		&goEchoExtractor{},
		&goFiberExtractor{},
		&goStdlibExtractor{},
		// .NET
		&aspnetMVCExtractor{},
		&aspnetMinimalExtractor{},
		// Java
		&springExtractor{},
		// Ruby
		&railsExtractor{},
		// PHP
		&laravelExtractor{},
		// Rust
		&actixExtractor{},
		&axumExtractor{},
		// Kotlin
		&ktorExtractor{},
	}
}

// All returns all registered extractors.
func All() []Extractor {
	result := make([]Extractor, len(registry))
	copy(result, registry)
	return result
}

// Run auto-detects frameworks (unless cfg.Frameworks is set), runs the
// appropriate extractors concurrently, and returns all discovered endpoints.
// Extractor panics are recovered and logged to stderr — one bad extractor
// must not abort the whole scan.
func Run(dir string, cfg *config.ScanConfig) ([]ScannedEndpoint, error) {
	var selected []Extractor

	if len(cfg.Frameworks) > 0 {
		// Use only the explicitly requested extractors.
		nameSet := make(map[string]bool, len(cfg.Frameworks))
		for _, f := range cfg.Frameworks {
			nameSet[f] = true
		}
		for _, ex := range registry {
			if nameSet[ex.Name()] {
				selected = append(selected, ex)
			}
		}
	} else {
		// Auto-detect.
		detected := DetectFrameworks(dir)
		nameSet := make(map[string]bool, len(detected))
		for _, f := range detected {
			nameSet[f] = true
		}
		for _, ex := range registry {
			if nameSet[ex.Name()] {
				selected = append(selected, ex)
			}
		}
	}

	// Fallback: try all extractors and keep any that return results.
	if len(selected) == 0 {
		selected = All()
	}

	// Run selected extractors concurrently.
	type result struct {
		endpoints []ScannedEndpoint
		err       error
		name      string
	}

	results := make(chan result, len(selected))
	var wg sync.WaitGroup

	for _, ex := range selected {
		wg.Add(1)
		go func(e Extractor) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					fmt.Fprintf(errorWriter, "scanner: extractor %q panicked: %v\n", e.Name(), r)
					results <- result{name: e.Name(), err: fmt.Errorf("panic: %v", r)}
				}
			}()
			endpoints, err := e.Extract(dir, cfg)
			results <- result{name: e.Name(), endpoints: endpoints, err: err}
		}(ex)
	}

	wg.Wait()
	close(results)

	var all []ScannedEndpoint
	for r := range results {
		if r.err != nil {
			fmt.Fprintf(errorWriter, "scanner: extractor %q error: %v\n", r.name, r.err)
			continue
		}
		all = append(all, r.endpoints...)
	}

	// Deduplicate by (Method, PathPattern), keeping the one with the most schema info.
	all = deduplicateEndpoints(all)

	// Sort by PathPattern then Method for deterministic output.
	sort.Slice(all, func(i, j int) bool {
		if all[i].PathPattern != all[j].PathPattern {
			return all[i].PathPattern < all[j].PathPattern
		}
		return all[i].Method < all[j].Method
	})

	return all, nil
}

// deduplicateEndpoints removes duplicate (Method, PathPattern) pairs, keeping
// the entry with the most schema information.
func deduplicateEndpoints(endpoints []ScannedEndpoint) []ScannedEndpoint {
	type key struct {
		method      string
		pathPattern string
	}
	best := make(map[key]ScannedEndpoint)

	for _, ep := range endpoints {
		k := key{method: ep.Method, pathPattern: ep.PathPattern}
		existing, ok := best[k]
		if !ok {
			best[k] = ep
			continue
		}
		// Keep the one with more schema information.
		if schemaScore(ep) > schemaScore(existing) {
			best[k] = ep
		}
	}

	result := make([]ScannedEndpoint, 0, len(best))
	for _, ep := range best {
		result = append(result, ep)
	}
	return result
}

// schemaScore returns a numeric score representing schema richness.
func schemaScore(ep ScannedEndpoint) int {
	score := 0
	if ep.ReqSchema != nil {
		score += 2
		if len(ep.ReqSchema.Properties) > 0 {
			score += len(ep.ReqSchema.Properties)
		}
	}
	if ep.RespSchema != nil {
		score += 2
		if len(ep.RespSchema.Properties) > 0 {
			score += len(ep.RespSchema.Properties)
		}
	}
	score += len(ep.Params)
	if ep.Description != "" {
		score++
	}
	if len(ep.Tags) > 0 {
		score++
	}
	return score
}

// reExpressParam matches Express/Rails-style :param syntax.
var reExpressParam = regexp.MustCompile(`:([A-Za-z_][A-Za-z0-9_]*)`)

// reAxumParam matches older Axum-style <param> syntax.
var reAxumParam = regexp.MustCompile(`<([A-Za-z_][A-Za-z0-9_]*)>`)

// NormalizeFrameworkPath converts framework path param syntax to OpenAPI {param} format.
// Handles: :param (Express/Rails/Sinatra), <param> (Axum older), {param} (already correct).
func NormalizeFrameworkPath(path string) string {
	// Replace :param → {param}
	path = reExpressParam.ReplaceAllString(path, `{$1}`)
	// Replace <param> → {param}
	path = reAxumParam.ReplaceAllString(path, `{$1}`)
	// Collapse duplicate slashes.
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	return path
}
