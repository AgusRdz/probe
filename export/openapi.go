package export

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/AgusRdz/probe/observer"
	"github.com/AgusRdz/probe/store"
	"gopkg.in/yaml.v3"
)

// OpenAPISpec is the top-level OpenAPI 3.0.3 document.
type OpenAPISpec struct {
	OpenAPI    string                     `yaml:"openapi" json:"openapi"`
	Info       OpenAPIInfo                `yaml:"info" json:"info"`
	Components *OpenAPIComponents         `yaml:"components,omitempty" json:"components,omitempty"`
	Paths      map[string]OpenAPIPathItem `yaml:"paths" json:"paths"`
}

// OpenAPIInfo holds the spec title and version.
type OpenAPIInfo struct {
	Title   string `yaml:"title" json:"title"`
	Version string `yaml:"version" json:"version"`
}

// OpenAPIPathItem groups all operations for a single path.
type OpenAPIPathItem struct {
	Get     *OpenAPIOperation `yaml:"get,omitempty" json:"get,omitempty"`
	Post    *OpenAPIOperation `yaml:"post,omitempty" json:"post,omitempty"`
	Put     *OpenAPIOperation `yaml:"put,omitempty" json:"put,omitempty"`
	Patch   *OpenAPIOperation `yaml:"patch,omitempty" json:"patch,omitempty"`
	Delete  *OpenAPIOperation `yaml:"delete,omitempty" json:"delete,omitempty"`
	Head    *OpenAPIOperation `yaml:"head,omitempty" json:"head,omitempty"`
	Options *OpenAPIOperation `yaml:"options,omitempty" json:"options,omitempty"`
}

// OpenAPIOperation describes a single HTTP operation.
type OpenAPIOperation struct {
	Summary     string                     `yaml:"summary,omitempty" json:"summary,omitempty"`
	Description string                     `yaml:"description,omitempty" json:"description,omitempty"`
	Tags        []string                   `yaml:"tags,omitempty" json:"tags,omitempty"`
	Deprecated  bool                       `yaml:"deprecated,omitempty" json:"deprecated,omitempty"`
	Parameters  []OpenAPIParameter         `yaml:"parameters,omitempty" json:"parameters,omitempty"`
	RequestBody *OpenAPIRequestBody        `yaml:"requestBody,omitempty" json:"requestBody,omitempty"`
	Responses   map[string]OpenAPIResponse `yaml:"responses" json:"responses"`
	Security    []map[string][]string      `yaml:"security,omitempty" json:"security,omitempty"`
}

// OpenAPIParameter represents a path or query parameter.
type OpenAPIParameter struct {
	Name     string       `yaml:"name" json:"name"`
	In       string       `yaml:"in" json:"in"` // "path" | "query"
	Required bool         `yaml:"required" json:"required"`
	Schema   OpenAPISchema `yaml:"schema" json:"schema"`
}

// OpenAPIRequestBody describes the request body content.
type OpenAPIRequestBody struct {
	Required bool                        `yaml:"required,omitempty" json:"required,omitempty"`
	Content  map[string]OpenAPIMediaType `yaml:"content" json:"content"`
}

// OpenAPIMediaType wraps a schema for a specific media type.
type OpenAPIMediaType struct {
	Schema OpenAPISchema `yaml:"schema" json:"schema"`
}

// OpenAPIResponse describes a single response by status code.
type OpenAPIResponse struct {
	Description string                      `yaml:"description" json:"description"`
	Content     map[string]OpenAPIMediaType `yaml:"content,omitempty" json:"content,omitempty"`
}

// OpenAPIComponents holds reusable spec components (security schemes, etc.).
type OpenAPIComponents struct {
	SecuritySchemes map[string]OpenAPISecurityScheme `yaml:"securitySchemes,omitempty" json:"securitySchemes,omitempty"`
}

// OpenAPISecurityScheme describes an authentication mechanism.
type OpenAPISecurityScheme struct {
	Type         string `yaml:"type" json:"type"`
	Scheme       string `yaml:"scheme,omitempty" json:"scheme,omitempty"`
	BearerFormat string `yaml:"bearerFormat,omitempty" json:"bearerFormat,omitempty"`
}

// OpenAPISchema represents a JSON Schema-compatible type definition.
type OpenAPISchema struct {
	Type        string                  `yaml:"type,omitempty" json:"type,omitempty"`
	Format      string                  `yaml:"format,omitempty" json:"format,omitempty"`
	Nullable    bool                    `yaml:"nullable,omitempty" json:"nullable,omitempty"`
	Properties  map[string]OpenAPISchema `yaml:"properties,omitempty" json:"properties,omitempty"`
	Items       *OpenAPISchema          `yaml:"items,omitempty" json:"items,omitempty"`
	Required    []string                `yaml:"required,omitempty" json:"required,omitempty"`
	Enum        []string                `yaml:"enum,omitempty" json:"enum,omitempty"`
	MinLength   int                     `yaml:"minLength,omitempty" json:"minLength,omitempty"`
	MaxLength   int                     `yaml:"maxLength,omitempty" json:"maxLength,omitempty"`
	Pattern     string                  `yaml:"pattern,omitempty" json:"pattern,omitempty"`
	Description string                  `yaml:"description,omitempty" json:"description,omitempty"`
}

// ExportOptions controls what gets included in the export.
type ExportOptions struct {
	Format              string  // "openapi" | "postman"
	MinCalls            int     // 0 = include all (scan + observed); 1 = observed only
	ConfidenceThreshold float64 // required vs optional field threshold (default 0.9)
	InfoTitle           string
	InfoVersion         string
}

var pathParamRe = regexp.MustCompile(`\{([^}]+)\}`)

// StoreReader is the read-only subset of store.Store used by export functions.
// Both GenerateOpenAPI and GeneratePostman accept this interface so tests can
// pass a real SQLite store without importing store directly.
type StoreReader interface {
	GetEndpoints() ([]store.Endpoint, error)
	GetFieldConfidence(endpointID int64) ([]store.FieldConfidenceRow, error)
	GetObservations(endpointID int64, limit int) ([]store.Observation, error)
	GetEndpointHeaders(endpointID int64) ([]store.HeaderRow, error)
	GetQueryParams(endpointID int64) ([]store.QueryParamRow, error)
	GetVariants(endpointID int64) ([]store.RequestVariant, error)
	GetVariantFieldConfidence(variantID int64) ([]store.FieldConfidenceRow, error)
}

// GenerateOpenAPI queries the store and builds an OpenAPISpec.
// Returns the spec, the count of exported endpoints, and any error.
func GenerateOpenAPI(s StoreReader, opts ExportOptions) (*OpenAPISpec, int, error) {
	if opts.ConfidenceThreshold == 0 {
		opts.ConfidenceThreshold = 0.9
	}
	if opts.InfoTitle == "" {
		opts.InfoTitle = "Discovered API"
	}
	if opts.InfoVersion == "" {
		opts.InfoVersion = "0.0.1"
	}

	endpoints, err := s.GetEndpoints()
	if err != nil {
		return nil, 0, fmt.Errorf("export: get endpoints: %w", err)
	}

	paths := make(map[string]OpenAPIPathItem)
	count := 0
	hasAuthEndpoint := false

	for _, ep := range endpoints {
		// Always skip gRPC — no schema support in Phase 1.
		if ep.Protocol == "grpc" {
			continue
		}
		if ep.CallCount < opts.MinCalls {
			continue
		}

		// Build path parameters from {param} segments.
		params := extractPathParams(ep.PathPattern)

		// Append query parameters observed from traffic.
		queryParamRows, err := s.GetQueryParams(ep.ID)
		if err != nil {
			return nil, 0, fmt.Errorf("export: get query params for endpoint %d: %w", ep.ID, err)
		}
		for _, qp := range queryParamRows {
			params = append(params, OpenAPIParameter{
				Name:     qp.ParamName,
				In:       "query",
				Required: false,
				Schema:   OpenAPISchema{Type: "string"},
			})
		}

		// Get field confidence for request body construction.
		fieldRows, err := s.GetFieldConfidence(ep.ID)
		if err != nil {
			return nil, 0, fmt.Errorf("export: get field confidence for endpoint %d: %w", ep.ID, err)
		}

		var reqBody *OpenAPIRequestBody
		reqSchema := buildSchemaFromConfidence(fieldRows, "request", opts.ConfidenceThreshold)
		if reqSchema != nil {
			reqBody = &OpenAPIRequestBody{
				Required: true,
				Content: map[string]OpenAPIMediaType{
					"application/json": {Schema: *reqSchema},
				},
			}
		}

		respSchema := buildSchemaFromConfidence(fieldRows, "response", opts.ConfidenceThreshold)

		// Collect unique status codes from observations.
		observations, err := s.GetObservations(ep.ID, 100)
		if err != nil {
			return nil, 0, fmt.Errorf("export: get observations for endpoint %d: %w", ep.ID, err)
		}

		responses := buildResponses(observations, respSchema)

		// Ensure there is always at least a default response entry.
		if len(responses) == 0 {
			resp := OpenAPIResponse{Description: statusDescription(200)}
			if respSchema != nil {
				resp.Content = map[string]OpenAPIMediaType{
					"application/json": {Schema: *respSchema},
				}
			}
			responses = map[string]OpenAPIResponse{"200": resp}
		}

		op := &OpenAPIOperation{
			Description: ep.Description,
			Tags:        ep.Tags,
			Deprecated:  ep.Deprecated,
			Parameters:  params,
			RequestBody: reqBody,
			Responses:   responses,
		}

		if ep.RequiresAuth && !isTokenGenerationPath(ep.PathPattern) {
			op.Security = []map[string][]string{{"bearerAuth": {}}}
			hasAuthEndpoint = true
		}

		pathItem := paths[ep.PathPattern]
		setOperation(&pathItem, ep.Method, op)
		paths[ep.PathPattern] = pathItem
		count++
	}

	spec := &OpenAPISpec{
		OpenAPI: "3.0.3",
		Info: OpenAPIInfo{
			Title:   opts.InfoTitle,
			Version: opts.InfoVersion,
		},
		Paths: paths,
	}
	if hasAuthEndpoint {
		spec.Components = &OpenAPIComponents{
			SecuritySchemes: map[string]OpenAPISecurityScheme{
				"bearerAuth": {
					Type:         "http",
					Scheme:       "bearer",
					BearerFormat: "JWT",
				},
			},
		}
	}

	return spec, count, nil
}

// WriteYAML writes the spec as YAML to w.
func WriteYAML(w io.Writer, spec *OpenAPISpec) error {
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	return enc.Encode(spec)
}

// WriteJSON writes the spec as indented JSON to w.
func WriteJSON(w io.Writer, spec *OpenAPISpec) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(spec)
}

// extractPathParams parses {param} segments from a path pattern and returns
// them as OpenAPIParameter slices (in: "path", required: true).
func extractPathParams(pathPattern string) []OpenAPIParameter {
	matches := pathParamRe.FindAllStringSubmatch(pathPattern, -1)
	if len(matches) == 0 {
		return nil
	}
	params := make([]OpenAPIParameter, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, m := range matches {
		name := m[1]
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		params = append(params, OpenAPIParameter{
			Name:     name,
			In:       "path",
			Required: true,
			Schema:   OpenAPISchema{Type: "string"},
		})
	}
	return params
}

// buildResponses collects unique status codes from observations and maps each
// to an OpenAPIResponse with an appropriate description.
// respSchema is attached to all 2xx responses when non-nil.
func buildResponses(observations []store.Observation, respSchema *OpenAPISchema) map[string]OpenAPIResponse {
	seen := make(map[int]struct{})
	for _, o := range observations {
		seen[o.StatusCode] = struct{}{}
	}
	if len(seen) == 0 {
		return nil
	}

	responses := make(map[string]OpenAPIResponse, len(seen))
	for code := range seen {
		key := fmt.Sprintf("%d", code)
		resp := OpenAPIResponse{Description: statusDescription(code)}
		if respSchema != nil && code >= 200 && code < 300 {
			resp.Content = map[string]OpenAPIMediaType{
				"application/json": {Schema: *respSchema},
			}
		}
		responses[key] = resp
	}
	return responses
}

// setOperation assigns op to the correct method field on item.
func setOperation(item *OpenAPIPathItem, method string, op *OpenAPIOperation) {
	switch strings.ToUpper(method) {
	case "GET":
		item.Get = op
	case "POST":
		item.Post = op
	case "PUT":
		item.Put = op
	case "PATCH":
		item.Patch = op
	case "DELETE":
		item.Delete = op
	case "HEAD":
		item.Head = op
	case "OPTIONS":
		item.Options = op
	}
}

// observerSchemaToOpenAPI converts an observer.Schema to OpenAPISchema.
func observerSchemaToOpenAPI(s observer.Schema) OpenAPISchema {
	out := OpenAPISchema{
		Type:        s.Type,
		Format:      s.Format,
		Nullable:    s.Nullable,
		Enum:        s.Enum,
		MinLength:   s.MinLength,
		MaxLength:   s.MaxLength,
		Pattern:     s.Pattern,
		Description: s.Description,
	}

	if s.Items != nil {
		items := observerSchemaToOpenAPI(*s.Items)
		out.Items = &items
	}

	if len(s.Properties) > 0 {
		out.Properties = make(map[string]OpenAPISchema, len(s.Properties))
		for k, v := range s.Properties {
			if v != nil {
				out.Properties[k] = observerSchemaToOpenAPI(*v)
			}
		}
	}

	if len(s.Required) > 0 {
		out.Required = append([]string(nil), s.Required...)
	}

	return out
}

// buildSchemaFromConfidence builds an OpenAPISchema from accumulated field
// confidence rows for a specific location ("request" or "response").
// Fields with confidence >= threshold are added to the parent's required list.
// Dot-notation paths (e.g. "user.address.city") produce nested object schemas.
func buildSchemaFromConfidence(fields []store.FieldConfidenceRow, location string, threshold float64) *OpenAPISchema {
	// Filter to the requested location.
	var filtered []store.FieldConfidenceRow
	for _, f := range fields {
		if f.Location == location {
			filtered = append(filtered, f)
		}
	}
	if len(filtered) == 0 {
		return nil
	}

	// Sort for deterministic output.
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].FieldPath < filtered[j].FieldPath
	})

	root := &OpenAPISchema{Type: "object"}
	if root.Properties == nil {
		root.Properties = make(map[string]OpenAPISchema)
	}

	for _, row := range filtered {
		var obsSchema observer.Schema
		if err := json.Unmarshal([]byte(row.TypeJSON), &obsSchema); err != nil {
			// Fallback to a plain string schema if we can't parse.
			obsSchema = observer.Schema{Type: "string"}
		}

		fieldSchema := observerSchemaToOpenAPI(obsSchema)
		confidence := 0.0
		if row.TotalCalls > 0 {
			confidence = float64(row.SeenCount) / float64(row.TotalCalls)
		}

		insertField(root, strings.Split(row.FieldPath, "."), fieldSchema, confidence >= threshold)
	}

	if len(root.Properties) == 0 {
		return nil
	}
	return root
}

// insertField recursively walks the path segments and inserts fieldSchema at
// the leaf, creating intermediate object nodes as needed. isRequired controls
// whether the leaf field is added to its parent's Required slice.
func insertField(node *OpenAPISchema, path []string, fieldSchema OpenAPISchema, isRequired bool) {
	if len(path) == 0 {
		return
	}

	key := path[0]

	if len(path) == 1 {
		// Leaf: set the field on this node.
		if node.Properties == nil {
			node.Properties = make(map[string]OpenAPISchema)
		}
		node.Properties[key] = fieldSchema
		if isRequired {
			node.Required = appendUnique(node.Required, key)
		}
		return
	}

	// Intermediate: ensure an object node exists for this key.
	if node.Properties == nil {
		node.Properties = make(map[string]OpenAPISchema)
	}
	child, exists := node.Properties[key]
	if !exists {
		child = OpenAPISchema{Type: "object", Properties: make(map[string]OpenAPISchema)}
	}
	insertField(&child, path[1:], fieldSchema, isRequired)
	node.Properties[key] = child
}

// appendUnique appends s to slice only if not already present.
func appendUnique(slice []string, s string) []string {
	for _, v := range slice {
		if v == s {
			return slice
		}
	}
	return append(slice, s)
}

// statusDescription returns a human-readable description for common HTTP status codes.
func statusDescription(code int) string {
	descriptions := map[int]string{
		100: "Continue",
		101: "Switching Protocols",
		200: "OK",
		201: "Created",
		202: "Accepted",
		204: "No Content",
		301: "Moved Permanently",
		302: "Found",
		304: "Not Modified",
		400: "Bad Request",
		401: "Unauthorized",
		403: "Forbidden",
		404: "Not Found",
		405: "Method Not Allowed",
		409: "Conflict",
		410: "Gone",
		422: "Unprocessable Entity",
		429: "Too Many Requests",
		500: "Internal Server Error",
		501: "Not Implemented",
		502: "Bad Gateway",
		503: "Service Unavailable",
		504: "Gateway Timeout",
	}
	if desc, ok := descriptions[code]; ok {
		return desc
	}
	return "Response"
}
