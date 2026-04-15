package observer

import (
	"encoding/json"
	"strings"
)

// InferGraphQLSchema parses a GraphQL request body (JSON format) and returns a Schema.
// Extracts:
//   - variables: parsed from "variables" JSON key if present (JSON object → Schema)
//   - operation: "query", "mutation", or "subscription" from the "query" string
//
// Returns a Schema with:
//   - Type: "object"
//   - Description: the operation type ("query", "mutation", "subscription", or "graphql")
//   - Properties: schema of "variables" if present (key "variables" → nested Schema)
//
// Returns nil if body is empty or not valid JSON.
// Values are NEVER stored — only type metadata.
func InferGraphQLSchema(body []byte) *Schema {
	if len(body) == 0 {
		return nil
	}

	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return nil
	}

	schema := &Schema{
		Type:        "object",
		Description: "graphql",
	}

	// Detect operation type from the "query" string.
	if queryVal, ok := obj["query"]; ok {
		if queryStr, ok := queryVal.(string); ok {
			schema.Description = detectGraphQLOperation(queryStr)
		}
	}

	// Extract variables schema.
	if varsVal, ok := obj["variables"]; ok {
		if varsVal != nil {
			s := InferJSONSchema(varsVal)
			if schema.Properties == nil {
				schema.Properties = make(map[string]*Schema)
			}
			schema.Properties["variables"] = &s
		}
	}

	return schema
}

// detectGraphQLOperation returns "query", "mutation", "subscription", or "graphql"
// by scanning for the first keyword token in the query string.
func detectGraphQLOperation(query string) string {
	trimmed := strings.TrimSpace(query)
	lower := strings.ToLower(trimmed)

	for _, op := range []string{"mutation", "subscription", "query"} {
		if strings.HasPrefix(lower, op) {
			return op
		}
	}

	// Shorthand query syntax: starts with "{" — still a query.
	if strings.HasPrefix(trimmed, "{") {
		return "query"
	}

	return "graphql"
}
