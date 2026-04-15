package scanner

import (
	"github.com/AgusRdz/probe/config"
	"github.com/AgusRdz/probe/observer"
)

// ExtractedParam represents a path or query parameter discovered via static analysis.
type ExtractedParam struct {
	Name     string
	In       string // "path" | "query" | "header"
	Required bool
	Schema   observer.Schema
}

// ScannedEndpoint is a single route discovered by static analysis.
type ScannedEndpoint struct {
	Method      string           // HTTP method or GraphQL op type
	PathPattern string           // already in {param} format
	Protocol    string           // "rest" | "graphql" | "grpc"
	Framework   string           // "express" | "nestjs" | "nextjs" | ...
	SourceFile  string           // absolute path to source file
	SourceLine  int
	ReqSchema   *observer.Schema // nil if not extractable
	RespSchema  *observer.Schema // nil if not extractable
	StatusCodes []int            // from annotations (e.g. @ProducesResponseType)
	Description string           // from JSDoc/docstring/XML doc comments
	Tags        []string         // from @ApiTags, namespace, group name
	Params      []ExtractedParam
	Deprecated  bool
}

// Extractor discovers routes from source files.
// Implementations must be safe to call concurrently with other Extractors.
type Extractor interface {
	// Name returns the framework identifier (e.g. "express", "nestjs").
	Name() string
	// Detect returns true if this extractor applies to the given directory.
	// Must be fast — only check for file existence, never read file content.
	Detect(dir string) bool
	// Extract walks dir and returns all discovered endpoints.
	// Must handle errors gracefully: one bad file must not abort the whole scan.
	Extract(dir string, cfg *config.ScanConfig) ([]ScannedEndpoint, error)
}
