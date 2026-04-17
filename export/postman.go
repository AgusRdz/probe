package export

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// PostmanCollection represents a Postman Collection v2.1 JSON document.
// Spec: https://schema.getpostman.com/json/collection/v2.1.0/collection.json
type PostmanCollection struct {
	Info PostmanInfo   `json:"info"`
	Item []PostmanItem `json:"item"`
}

// PostmanInfo holds the collection name and schema URL.
type PostmanInfo struct {
	Name   string `json:"name"`
	Schema string `json:"schema"`
}

// PostmanItem represents a single request in the collection.
type PostmanItem struct {
	Name    string        `json:"name"`
	Request PostmanRequest `json:"request"`
}

// PostmanRequest describes the HTTP request.
type PostmanRequest struct {
	Method string          `json:"method"`
	Header []PostmanHeader `json:"header"`
	URL    PostmanURL      `json:"url"`
	Body   *PostmanBody    `json:"body,omitempty"`
}

// PostmanHeader is a single HTTP header key/value pair.
type PostmanHeader struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// PostmanURL holds the raw URL and its decomposed parts.
type PostmanURL struct {
	Raw      string              `json:"raw"`
	Host     []string            `json:"host"`
	Path     []string            `json:"path"`
	Variable []PostmanVariable   `json:"variable,omitempty"`
	Query    []PostmanQueryParam `json:"query,omitempty"`
}

// PostmanVariable is a path-level variable (path parameter).
type PostmanVariable struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

// PostmanBody describes the request body.
type PostmanBody struct {
	Mode    string              `json:"mode"`
	Raw     string              `json:"raw,omitempty"`
	Options *PostmanBodyOptions `json:"options,omitempty"`
}

// PostmanBodyOptions carries the raw body language hint.
type PostmanBodyOptions struct {
	Raw PostmanBodyRaw `json:"raw"`
}

// PostmanBodyRaw names the language used in the raw body editor.
type PostmanBodyRaw struct {
	Language string `json:"language"`
}

// PostmanQueryParam is a URL query parameter.
type PostmanQueryParam struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

const postmanSchemaURL = "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"

// GeneratePostman builds a Postman Collection v2.1 from the store's endpoints.
// Uses the same ExportOptions as GenerateOpenAPI (InfoTitle becomes collection name).
func GeneratePostman(s StoreReader, opts ExportOptions) (*PostmanCollection, error) {
	if opts.ConfidenceThreshold == 0 {
		opts.ConfidenceThreshold = 0.9
	}
	if opts.InfoTitle == "" {
		opts.InfoTitle = "Discovered API"
	}

	endpoints, err := s.GetEndpoints()
	if err != nil {
		return nil, fmt.Errorf("postman: get endpoints: %w", err)
	}

	items := make([]PostmanItem, 0, len(endpoints))

	for _, ep := range endpoints {
		if ep.Protocol == "grpc" {
			continue
		}
		if ep.CallCount < opts.MinCalls {
			continue
		}

		fieldRows, err := s.GetFieldConfidence(ep.ID)
		if err != nil {
			return nil, fmt.Errorf("postman: get field confidence for endpoint %d: %w", ep.ID, err)
		}

		headerRows, err := s.GetEndpointHeaders(ep.ID)
		if err != nil {
			return nil, fmt.Errorf("postman: get headers for endpoint %d: %w", ep.ID, err)
		}

		queryParamRows, err := s.GetQueryParams(ep.ID)
		if err != nil {
			return nil, fmt.Errorf("postman: get query params for endpoint %d: %w", ep.ID, err)
		}

		url, variables := buildPostmanURL(ep.PathPattern)

		var headers []PostmanHeader
		var body *PostmanBody

		// Add observed request headers (Content-Type added below only if body present).
		for _, hr := range headerRows {
			lower := strings.ToLower(hr.HeaderName)
			if lower == "content-type" {
				continue // handled with body below
			}
			headers = append(headers, PostmanHeader{
				Key:   hr.HeaderName,
				Value: headerPlaceholder(hr.HeaderName),
			})
		}

		// Add Authorization header for auth-required endpoints not already covered
		// by observed traffic headers.
		if ep.RequiresAuth {
			hasAuth := false
			for _, h := range headers {
				if strings.ToLower(h.Key) == "authorization" {
					hasAuth = true
					break
				}
			}
			if !hasAuth {
				headers = append(headers, PostmanHeader{
					Key:   "Authorization",
					Value: "Bearer {{token}}",
				})
			}
		}

		reqSchema := buildSchemaFromConfidence(fieldRows, "request", opts.ConfidenceThreshold)
		if reqSchema != nil {
			template := schemaToJSONTemplate(reqSchema)
			raw, _ := json.MarshalIndent(template, "", "  ")
			headers = append(headers, PostmanHeader{Key: "Content-Type", Value: "application/json"})
			body = &PostmanBody{
				Mode: "raw",
				Raw:  string(raw),
				Options: &PostmanBodyOptions{
					Raw: PostmanBodyRaw{Language: "json"},
				},
			}
		}

		// Build query params.
		var queryParams []PostmanQueryParam
		for _, qp := range queryParamRows {
			queryParams = append(queryParams, PostmanQueryParam{
				Key:   qp.ParamName,
				Value: "",
			})
		}

		items = append(items, PostmanItem{
			Name: fmt.Sprintf("%s %s", strings.ToUpper(ep.Method), ep.PathPattern),
			Request: PostmanRequest{
				Method: strings.ToUpper(ep.Method),
				Header: headers,
				URL:    PostmanURL{Raw: url, Host: []string{"{{baseUrl}}"}, Path: buildPathSegments(ep.PathPattern), Variable: variables, Query: queryParams},
				Body:   body,
			},
		})
	}

	return &PostmanCollection{
		Info: PostmanInfo{
			Name:   opts.InfoTitle,
			Schema: postmanSchemaURL,
		},
		Item: items,
	}, nil
}

// WritePostman writes the collection as indented JSON to w.
func WritePostman(w io.Writer, c *PostmanCollection) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(c)
}

// buildPostmanURL converts a path pattern like /users/{id}/posts to:
//   - raw URL: {{baseUrl}}/users/{{id}}/posts
//   - variable list: [{Key: "id", Value: ""}]
func buildPostmanURL(pathPattern string) (raw string, variables []PostmanVariable) {
	segments := strings.Split(strings.TrimPrefix(pathPattern, "/"), "/")
	out := make([]string, 0, len(segments))
	for _, seg := range segments {
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			name := seg[1 : len(seg)-1]
			out = append(out, "{{"+name+"}}")
			variables = append(variables, PostmanVariable{Key: name, Value: ""})
		} else {
			out = append(out, seg)
		}
	}
	raw = "{{baseUrl}}/" + strings.Join(out, "/")
	return raw, variables
}

// buildPathSegments splits a path pattern into its segments (without leading slash).
func buildPathSegments(pathPattern string) []string {
	trimmed := strings.TrimPrefix(pathPattern, "/")
	if trimmed == "" {
		return []string{}
	}
	return strings.Split(trimmed, "/")
}

// headerPlaceholder returns a safe placeholder value for a request header name.
// Sensitive headers (auth, keys, tokens) get {{variable}} placeholders.
// Well-known headers get their conventional default values.
func headerPlaceholder(name string) string {
	lower := strings.ToLower(name)
	switch lower {
	case "authorization":
		return "Bearer {{token}}"
	case "x-api-key", "x-apikey", "api-key", "x-api-token":
		return "{{api_key}}"
	case "x-auth-token", "x-access-token", "x-token":
		return "{{token}}"
	case "accept":
		return "application/json"
	case "content-type":
		return "application/json"
	default:
		// Convert to snake_case placeholder: "X-Tenant-ID" → "{{x_tenant_id}}"
		slug := strings.NewReplacer("-", "_", " ", "_").Replace(lower)
		return "{{" + slug + "}}"
	}
}

// schemaToJSONTemplate recursively converts an OpenAPISchema into a Go value
// with placeholder values suitable for a JSON body template.
func schemaToJSONTemplate(s *OpenAPISchema) interface{} {
	if s == nil {
		return nil
	}
	switch s.Type {
	case "object":
		obj := make(map[string]interface{}, len(s.Properties))
		for k, v := range s.Properties {
			v := v // capture
			obj[k] = schemaToJSONTemplate(&v)
		}
		return obj
	case "array":
		return []interface{}{}
	case "integer":
		return 0
	case "number":
		return 0.0
	case "boolean":
		return false
	default:
		return ""
	}
}
