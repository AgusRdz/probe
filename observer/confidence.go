package observer

// FieldConfidence holds computed confidence for a single field.
type FieldConfidence struct {
	FieldPath  string
	Location   string // "request" | "response"
	SeenCount  int
	TotalCalls int
	Schema     Schema
}

// Confidence returns the ratio of seen_count / total_calls.
// Returns 0 if TotalCalls == 0.
func (f FieldConfidence) Confidence() float64 {
	if f.TotalCalls == 0 {
		return 0
	}
	return float64(f.SeenCount) / float64(f.TotalCalls)
}

// IsRequired returns true if Confidence() >= threshold.
func (f FieldConfidence) IsRequired(threshold float64) bool {
	return f.Confidence() >= threshold
}

// OverallConfidence computes the schema-level confidence for an endpoint:
// the average Confidence() across all fields for the given location.
// Returns 0 if no fields match the location.
func OverallConfidence(fields []FieldConfidence, location string) float64 {
	var sum float64
	count := 0
	for _, f := range fields {
		if f.Location == location {
			sum += f.Confidence()
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

// MergeSchemaWithConfidence takes the raw inferred Schema from the latest observation
// and the accumulated FieldConfidence rows, and returns a Schema where:
// - field.Required slice is populated based on threshold
// - nested objects recurse correctly
// Used by export/openapi.go to build the final spec.
func MergeSchemaWithConfidence(schema *Schema, fields []FieldConfidence, location string, threshold float64) *Schema {
	if schema == nil {
		return nil
	}

	// Build a lookup map from field_path → FieldConfidence for quick access.
	lookup := make(map[string]FieldConfidence, len(fields))
	for _, f := range fields {
		if f.Location == location {
			lookup[f.FieldPath] = f
		}
	}

	result := mergeSchemaNode(schema, "", lookup, threshold)
	return result
}

// mergeSchemaNode recurses through the schema tree, populating Required based on
// field confidence data. prefix is the dot-notation path up to this node.
func mergeSchemaNode(schema *Schema, prefix string, lookup map[string]FieldConfidence, threshold float64) *Schema {
	if schema == nil {
		return nil
	}

	// Shallow copy so we don't mutate the original.
	copy := *schema

	if copy.Type == "object" && len(copy.Properties) > 0 {
		newProps := make(map[string]*Schema, len(copy.Properties))
		required := make([]string, 0)

		for key, propSchema := range copy.Properties {
			fieldPath := key
			if prefix != "" {
				fieldPath = prefix + "." + key
			}

			// Recurse into nested objects.
			merged := mergeSchemaNode(propSchema, fieldPath, lookup, threshold)
			newProps[key] = merged

			// Determine required status from confidence data if available;
			// otherwise fall back to the original schema's Required list.
			if fc, ok := lookup[fieldPath]; ok {
				if fc.IsRequired(threshold) {
					required = append(required, key)
				}
			} else {
				// No confidence data for this field — preserve original required annotation.
				for _, r := range schema.Required {
					if r == key {
						required = append(required, key)
						break
					}
				}
			}
		}

		copy.Properties = newProps
		if len(required) > 0 {
			copy.Required = required
		} else {
			copy.Required = nil
		}
	}

	if copy.Type == "array" && copy.Items != nil {
		itemPrefix := prefix
		if itemPrefix != "" {
			itemPrefix = prefix + "[]"
		}
		merged := mergeSchemaNode(copy.Items, itemPrefix, lookup, threshold)
		copy.Items = merged
	}

	return &copy
}
