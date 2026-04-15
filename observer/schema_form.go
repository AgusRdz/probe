package observer

import (
	"net/url"
	"strconv"
	"strings"
)

// InferFormBody parses application/x-www-form-urlencoded bodies.
// Each key becomes a property. Values are analyzed:
//   - "true"/"false" → boolean
//   - parseable as integer → integer
//   - parseable as float → number
//   - otherwise → string
//
// Array keys (name[]) are treated as arrays of the inferred type.
// Returns nil if body is empty or not parseable.
// Values are NEVER stored — only type metadata.
func InferFormBody(data []byte) *Schema {
	if len(data) == 0 {
		return nil
	}

	values, err := url.ParseQuery(string(data))
	if err != nil || len(values) == 0 {
		return nil
	}

	props := make(map[string]*Schema, len(values))

	for rawKey, vals := range values {
		isArray := strings.HasSuffix(rawKey, "[]")
		key := strings.TrimSuffix(rawKey, "[]")

		// Find first non-empty value to infer type from.
		var sample string
		for _, v := range vals {
			if v != "" {
				sample = v
				break
			}
		}

		fieldSchema := inferFormValue(sample)

		if isArray {
			itemCopy := fieldSchema
			props[key] = &Schema{Type: "array", Items: &itemCopy}
		} else {
			cp := fieldSchema
			props[key] = &cp
		}
	}

	return &Schema{Type: "object", Properties: props}
}

// inferFormValue returns the inferred Schema for a single form field value.
func inferFormValue(v string) Schema {
	lower := strings.ToLower(v)
	if lower == "true" || lower == "false" {
		return Schema{Type: "boolean"}
	}
	if _, err := strconv.ParseInt(v, 10, 64); err == nil {
		return Schema{Type: "integer"}
	}
	if _, err := strconv.ParseFloat(v, 64); err == nil {
		return Schema{Type: "number"}
	}
	return Schema{Type: "string"}
}
