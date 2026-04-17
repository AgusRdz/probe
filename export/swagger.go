package export

import (
	"fmt"
	"io"
	"strings"

	"github.com/AgusRdz/probe/store"
	"gopkg.in/yaml.v3"
)

// SwaggerSpec is the top-level Swagger 2.0 document.
type SwaggerSpec struct {
	Swagger  string                     `yaml:"swagger"`
	Info     OpenAPIInfo                `yaml:"info"`
	Host     string                     `yaml:"host"`
	BasePath string                     `yaml:"basePath"`
	Schemes  []string                   `yaml:"schemes"`
	Paths    map[string]SwaggerPathItem `yaml:"paths"`
}

// SwaggerPathItem groups all operations for a single path.
type SwaggerPathItem struct {
	Get     *SwaggerOperation `yaml:"get,omitempty"`
	Post    *SwaggerOperation `yaml:"post,omitempty"`
	Put     *SwaggerOperation `yaml:"put,omitempty"`
	Patch   *SwaggerOperation `yaml:"patch,omitempty"`
	Delete  *SwaggerOperation `yaml:"delete,omitempty"`
	Head    *SwaggerOperation `yaml:"head,omitempty"`
	Options *SwaggerOperation `yaml:"options,omitempty"`
}

// SwaggerOperation describes a single HTTP operation.
type SwaggerOperation struct {
	Summary     string                     `yaml:"summary,omitempty"`
	Description string                     `yaml:"description,omitempty"`
	Tags        []string                   `yaml:"tags,omitempty"`
	Deprecated  bool                       `yaml:"deprecated,omitempty"`
	Consumes    []string                   `yaml:"consumes,omitempty"`
	Produces    []string                   `yaml:"produces,omitempty"`
	Parameters  []SwaggerParameter         `yaml:"parameters,omitempty"`
	Responses   map[string]SwaggerResponse `yaml:"responses"`
}

// SwaggerParameter represents a path, body, or query parameter.
type SwaggerParameter struct {
	Name     string         `yaml:"name"`
	In       string         `yaml:"in"` // "path" | "body" | "query"
	Required bool           `yaml:"required"`
	Schema   *OpenAPISchema `yaml:"schema,omitempty"` // for in:body
	Type     string         `yaml:"type,omitempty"`   // for in:path/query
}

// SwaggerResponse describes a single response by status code.
type SwaggerResponse struct {
	Description string         `yaml:"description"`
	Schema      *OpenAPISchema `yaml:"schema,omitempty"`
}

// GenerateSwagger queries the store and builds a Swagger 2.0 spec.
func GenerateSwagger(s StoreReader, opts ExportOptions) (*SwaggerSpec, error) {
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
		return nil, fmt.Errorf("swagger: get endpoints: %w", err)
	}

	paths := make(map[string]SwaggerPathItem)

	for _, ep := range endpoints {
		if ep.Protocol == "grpc" {
			continue
		}
		if ep.CallCount < opts.MinCalls {
			continue
		}

		method := strings.ToUpper(ep.Method)

		fieldRows, err := s.GetFieldConfidence(ep.ID)
		if err != nil {
			return nil, fmt.Errorf("swagger: get field confidence for endpoint %d: %w", ep.ID, err)
		}

		// Build path parameters.
		var params []SwaggerParameter
		pathMatches := pathParamRe.FindAllStringSubmatch(ep.PathPattern, -1)
		seenParams := make(map[string]struct{}, len(pathMatches))
		for _, m := range pathMatches {
			name := m[1]
			if _, dup := seenParams[name]; dup {
				continue
			}
			seenParams[name] = struct{}{}
			params = append(params, SwaggerParameter{
				Name:     name,
				In:       "path",
				Required: true,
				Type:     "string",
			})
		}

		// Build request body as in:body parameter.
		reqSchema := buildSchemaFromConfidence(fieldRows, "request", opts.ConfidenceThreshold)
		var consumes, produces []string
		if reqSchema != nil {
			params = append(params, SwaggerParameter{
				Name:     "body",
				In:       "body",
				Required: true,
				Schema:   reqSchema,
			})
			consumes = []string{"application/json"}
		}

		// Collect unique status codes from observations.
		observations, err := s.GetObservations(ep.ID, 100)
		if err != nil {
			return nil, fmt.Errorf("swagger: get observations for endpoint %d: %w", ep.ID, err)
		}

		respSchema := buildSchemaFromConfidence(fieldRows, "response", opts.ConfidenceThreshold)
		if respSchema != nil {
			produces = []string{"application/json"}
		}

		responses := buildSwaggerResponses(observations, respSchema)
		if len(responses) == 0 {
			resp := SwaggerResponse{Description: statusDescription(200)}
			if respSchema != nil {
				resp.Schema = respSchema
			}
			responses = map[string]SwaggerResponse{"200": resp}
		}

		op := &SwaggerOperation{
			Description: ep.Description,
			Tags:        ep.Tags,
			Deprecated:  ep.Deprecated,
			Consumes:    consumes,
			Produces:    produces,
			Parameters:  params,
			Responses:   responses,
		}

		pathItem := paths[ep.PathPattern]
		setSwaggerOperation(&pathItem, method, op)
		paths[ep.PathPattern] = pathItem
	}

	spec := &SwaggerSpec{
		Swagger: "2.0",
		Info: OpenAPIInfo{
			Title:   opts.InfoTitle,
			Version: opts.InfoVersion,
		},
		Host:     "localhost",
		BasePath: "/",
		Schemes:  []string{"http"},
		Paths:    paths,
	}

	return spec, nil
}

// WriteSwaggerYAML writes the Swagger 2.0 spec as YAML to w.
func WriteSwaggerYAML(w io.Writer, spec *SwaggerSpec) error {
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	return enc.Encode(spec)
}

// buildSwaggerResponses collects unique status codes from observations and maps
// each to a SwaggerResponse. respSchema is attached to 2xx responses when non-nil.
func buildSwaggerResponses(observations []store.Observation, respSchema *OpenAPISchema) map[string]SwaggerResponse {
	seen := make(map[int]struct{})
	for _, o := range observations {
		seen[o.StatusCode] = struct{}{}
	}
	if len(seen) == 0 {
		return nil
	}
	responses := make(map[string]SwaggerResponse, len(seen))
	for code := range seen {
		key := fmt.Sprintf("%d", code)
		resp := SwaggerResponse{Description: statusDescription(code)}
		if respSchema != nil && code >= 200 && code < 300 {
			resp.Schema = respSchema
		}
		responses[key] = resp
	}
	return responses
}

// setSwaggerOperation assigns op to the correct method field on item.
func setSwaggerOperation(item *SwaggerPathItem, method string, op *SwaggerOperation) {
	switch method {
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
