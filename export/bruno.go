package export

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// BrunoCollection is a map of relative file paths to their contents.
// Keys are relative paths within the collection directory (e.g. "bruno.json", "get-api-users.bru").
type BrunoCollection map[string][]byte

var multiDashRe = regexp.MustCompile(`-{2,}`)

// GenerateBruno builds a Bruno API collection from the store's endpoints.
// Returns a BrunoCollection where keys are relative file paths.
func GenerateBruno(s StoreReader, opts ExportOptions) (BrunoCollection, error) {
	if opts.ConfidenceThreshold == 0 {
		opts.ConfidenceThreshold = 0.9
	}
	if opts.InfoTitle == "" {
		opts.InfoTitle = "Discovered API"
	}

	endpoints, err := s.GetEndpoints()
	if err != nil {
		return nil, fmt.Errorf("bruno: get endpoints: %w", err)
	}

	collection := make(BrunoCollection)

	// Write bruno.json manifest.
	manifest := map[string]interface{}{
		"version": "1",
		"name":    opts.InfoTitle,
		"type":    "collection",
		"ignore":  []interface{}{},
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("bruno: marshal manifest: %w", err)
	}
	collection["bruno.json"] = append(manifestBytes, '\n')

	seq := 0
	for _, ep := range endpoints {
		if ep.Protocol == "grpc" {
			continue
		}
		if ep.CallCount < opts.MinCalls {
			continue
		}

		seq++
		method := strings.ToUpper(ep.Method)

		fieldRows, err := s.GetFieldConfidence(ep.ID)
		if err != nil {
			return nil, fmt.Errorf("bruno: get field confidence for endpoint %d: %w", ep.ID, err)
		}

		reqSchema := buildSchemaFromConfidence(fieldRows, "request", opts.ConfidenceThreshold)
		hasBody := reqSchema != nil && (method == "POST" || method == "PUT" || method == "PATCH")

		// Bruno uses :param style; convert {id} → :id in URLs.
		brunoURL := pathParamRe.ReplaceAllStringFunc(ep.PathPattern, func(match string) string {
			name := match[1 : len(match)-1]
			return ":" + name
		})

		var buf bytes.Buffer
		fmt.Fprintf(&buf, "meta {\n")
		fmt.Fprintf(&buf, "  name: %s %s\n", method, ep.PathPattern)
		fmt.Fprintf(&buf, "  type: http\n")
		fmt.Fprintf(&buf, "  seq: %d\n", seq)
		fmt.Fprintf(&buf, "}\n\n")

		methodLower := strings.ToLower(method)
		if hasBody {
			fmt.Fprintf(&buf, "%s {\n", methodLower)
			fmt.Fprintf(&buf, "  url: {{baseUrl}}%s\n", brunoURL)
			fmt.Fprintf(&buf, "  body: json\n")
			fmt.Fprintf(&buf, "  auth: none\n")
			fmt.Fprintf(&buf, "}\n\n")

			template := schemaToJSONTemplate(reqSchema)
			raw, _ := json.MarshalIndent(template, "  ", "  ")
			fmt.Fprintf(&buf, "body:json {\n")
			fmt.Fprintf(&buf, "  %s\n", string(raw))
			fmt.Fprintf(&buf, "}\n")
		} else {
			fmt.Fprintf(&buf, "%s {\n", methodLower)
			fmt.Fprintf(&buf, "  url: {{baseUrl}}%s\n", brunoURL)
			fmt.Fprintf(&buf, "  body: none\n")
			fmt.Fprintf(&buf, "  auth: none\n")
			fmt.Fprintf(&buf, "}\n")
		}

		filename := brunoSlug(method, ep.PathPattern) + ".bru"
		collection[filename] = buf.Bytes()
	}

	return collection, nil
}

// brunoSlug generates a filename slug from a method and path pattern.
// Example: GET /api/users/{id} → get-api-users-id
func brunoSlug(method, pathPattern string) string {
	s := strings.ToLower(method) + "-" + pathPattern
	// Replace path separators and brace chars with dashes.
	s = strings.NewReplacer("/", "-", "{", "-", "}", "-").Replace(s)
	// Collapse multiple dashes.
	s = multiDashRe.ReplaceAllString(s, "-")
	// Trim leading/trailing dashes.
	s = strings.Trim(s, "-")
	return s
}
