package observer

import (
	"encoding/json"
	"math"
	"regexp"
	"sort"
	"strings"
)

var (
	reUUID     = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	reEmail    = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	reDateTime = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$`)
	reDate     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	reIPv4     = regexp.MustCompile(`^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$`)
)

// detectStringFormat returns the OpenAPI format string for well-known string patterns.
// Detects (in order): uuid, email, date-time, date, uri, ipv4, ipv6.
// Returns "" if no format detected.
func detectStringFormat(s string) string {
	switch {
	case reUUID.MatchString(s):
		return "uuid"
	case reEmail.MatchString(s):
		return "email"
	case reDateTime.MatchString(s):
		return "date-time"
	case reDate.MatchString(s):
		return "date"
	case strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://"):
		return "uri"
	case reIPv4.MatchString(s):
		return "ipv4"
	case len(s) >= 2 && strings.Contains(s, ":") && !strings.Contains(s, "://"):
		return "ipv6"
	}
	return ""
}

// InferJSONSchema converts a parsed JSON value (from json.Unmarshal into any)
// into a Schema. Handles: map[string]any, []any, string, float64, bool, nil.
// For arrays: merges schemas of ALL items (union of fields) for accurate item schema.
// For strings: calls detectStringFormat to detect email/uuid/date-time/uri/ipv4/ipv6.
// Values are NEVER stored — only type and format metadata.
func InferJSONSchema(v any) Schema {
	if v == nil {
		return Schema{Nullable: true}
	}

	switch val := v.(type) {
	case bool:
		return Schema{Type: "boolean"}

	case float64:
		if val == math.Trunc(val) {
			return Schema{Type: "integer"}
		}
		return Schema{Type: "number"}

	case string:
		return Schema{Type: "string", Format: detectStringFormat(val)}

	case []any:
		if len(val) == 0 {
			return Schema{Type: "array", Items: &Schema{Type: "object"}}
		}
		if len(val) == 1 {
			s := InferJSONSchema(val[0])
			return Schema{Type: "array", Items: &s}
		}
		merged := mergeItemSchemas(val)
		return Schema{Type: "array", Items: &merged}

	case map[string]any:
		return inferObjectSchema(val)
	}

	return Schema{Type: "string"}
}

// inferObjectSchema builds a Schema from a map[string]any.
func inferObjectSchema(obj map[string]any) Schema {
	if len(obj) == 0 {
		return Schema{Type: "object"}
	}

	props := make(map[string]*Schema, len(obj))
	required := make([]string, 0, len(obj))

	// Collect and sort keys for deterministic output.
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		s := InferJSONSchema(obj[k])
		sCopy := s
		props[k] = &sCopy
		required = append(required, k)
	}

	return Schema{
		Type:       "object",
		Properties: props,
		Required:   required,
	}
}

// mergeItemSchemas merges schemas of all array items into a single Schema.
// Union of all fields; a field is Required only if ALL items contain it.
func mergeItemSchemas(items []any) Schema {
	if len(items) == 0 {
		return Schema{Type: "object"}
	}

	// Collect per-item schemas.
	schemas := make([]Schema, len(items))
	for i, item := range items {
		schemas[i] = InferJSONSchema(item)
	}

	// If items are not objects, return the schema of the first item
	// (mixed arrays use first-item type as the representative schema).
	allObjects := true
	for _, s := range schemas {
		if s.Type != "object" {
			allObjects = false
			break
		}
	}
	if !allObjects {
		return schemas[0]
	}

	// Merge object schemas: union properties, required only if present in ALL.
	fieldCount := make(map[string]int)
	fieldSchemas := make(map[string]*Schema)

	for _, s := range schemas {
		for k, v := range s.Properties {
			fieldCount[k]++
			if _, exists := fieldSchemas[k]; !exists {
				vCopy := *v
				fieldSchemas[k] = &vCopy
			}
		}
	}

	required := make([]string, 0)
	for k, count := range fieldCount {
		if count == len(schemas) {
			required = append(required, k)
		}
	}
	sort.Strings(required)

	if len(fieldSchemas) == 0 {
		return Schema{Type: "object"}
	}

	var req []string
	if len(required) > 0 {
		req = required
	}

	return Schema{
		Type:       "object",
		Properties: fieldSchemas,
		Required:   req,
	}
}

// InferJSONBody parses raw JSON bytes and returns the inferred Schema.
// Returns nil if bytes are empty or not valid JSON.
func InferJSONBody(data []byte) *Schema {
	if len(data) == 0 {
		return nil
	}

	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil
	}

	s := InferJSONSchema(v)
	return &s
}
