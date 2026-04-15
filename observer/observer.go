package observer

import (
	"bytes"
	"strings"
)

// Extract takes a CapturedPair and returns the inferred request and response schemas.
// Dispatches based on ReqContentType and RespContentType.
// Returns (nil, nil) if both bodies are empty.
// NEVER modifies the pair — read-only.
func Extract(pair CapturedPair) (reqSchema *Schema, respSchema *Schema) {
	if len(pair.ReqBody) == 0 && len(pair.RespBody) == 0 {
		return nil, nil
	}

	reqSchema = inferByContentType(pair.ReqContentType, pair.ReqBody)
	respSchema = inferByContentType(pair.RespContentType, pair.RespBody)

	// GraphQL override: JSON content-type + body contains "query" key → annotate.
	if isJSON(pair.ReqContentType) && isGraphQLBody(pair.ReqBody) {
		if reqSchema != nil {
			desc := "graphql"
			reqSchema.Description = desc
		}
		if respSchema != nil {
			respSchema.Description = "graphql"
		}
	}

	return reqSchema, respSchema
}

// inferByContentType dispatches schema extraction for a single body based on its
// Content-Type header value.
func inferByContentType(contentType string, body []byte) *Schema {
	if len(body) == 0 {
		return nil
	}

	ct := strings.ToLower(strings.TrimSpace(contentType))
	// Strip parameters (e.g. "; charset=utf-8").
	if idx := strings.IndexByte(ct, ';'); idx != -1 {
		ct = strings.TrimSpace(ct[:idx])
	}

	switch {
	case ct == "application/json":
		return InferJSONBody(body)

	case strings.Contains(ct, "application/graphql"):
		return InferJSONBody(body)

	case ct == "application/x-www-form-urlencoded":
		return InferFormBody(body)

	case strings.HasPrefix(ct, "multipart/form-data"):
		return &Schema{Type: "object"}

	case ct == "application/xml" || ct == "text/xml":
		return &Schema{Type: "object", Description: "xml"}

	case strings.HasPrefix(ct, "application/grpc"):
		return &Schema{Type: "object", Description: "grpc"}

	default:
		// Empty or unrecognized: attempt JSON parse, nil if not valid JSON.
		return InferJSONBody(body)
	}
}

// InferFormBody parses application/x-www-form-urlencoded bodies.
// All values are inferred as string (form fields are always strings).
// Stub for Phase 1.
func InferFormBody(_ []byte) *Schema {
	return &Schema{Type: "object"}
}

// DetectProtocol returns the protocol string ("rest", "graphql", "grpc", "xml", "form")
// based on content type and body sniffing.
func DetectProtocol(reqContentType string, reqBody []byte) string {
	ct := strings.ToLower(strings.TrimSpace(reqContentType))
	if idx := strings.IndexByte(ct, ';'); idx != -1 {
		ct = strings.TrimSpace(ct[:idx])
	}

	if strings.HasPrefix(ct, "application/grpc") {
		return "grpc"
	}
	if ct == "application/xml" || ct == "text/xml" {
		return "xml"
	}
	if ct == "application/x-www-form-urlencoded" {
		return "form"
	}
	if strings.Contains(ct, "application/graphql") {
		return "graphql"
	}
	if isJSON(ct) && isGraphQLBody(reqBody) {
		return "graphql"
	}
	return "rest"
}

// isJSON returns true if the content type is application/json.
func isJSON(contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if idx := strings.IndexByte(ct, ';'); idx != -1 {
		ct = strings.TrimSpace(ct[:idx])
	}
	return ct == "application/json"
}

// isGraphQLBody returns true if the body is a JSON object containing a "query" key.
func isGraphQLBody(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	// Fast path: look for `"query"` as a JSON key without full unmarshal.
	return bytes.Contains(body, []byte(`"query"`))
}
