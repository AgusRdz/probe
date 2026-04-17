package export

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadExistingYAMLMap reads a YAML file into a generic map, preserving all
// top-level fields.
func LoadExistingYAMLMap(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("openapi merge: read %s: %w", path, err)
	}
	var m map[string]interface{}
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("openapi merge: parse yaml %s: %w", path, err)
	}
	return m, nil
}

// LoadExistingJSONMap reads a JSON file into a generic map.
func LoadExistingJSONMap(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("openapi merge: read %s: %w", path, err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("openapi merge: parse json %s: %w", path, err)
	}
	return m, nil
}

// MergeOpenAPIPaths adds missing path+method entries from incoming into
// existing. existing is the parsed YAML/JSON map of the full spec.
// Returns the list of "METHOD /path" keys that were added.
// Existing path+method entries are NEVER modified.
func MergeOpenAPIPaths(existing map[string]interface{}, incoming *OpenAPISpec) []string {
	// Ensure paths key is present and is map[string]interface{}.
	pathsRaw, _ := existing["paths"]
	existingPaths, _ := pathsRaw.(map[string]interface{})
	if existingPaths == nil {
		existingPaths = make(map[string]interface{})
		existing["paths"] = existingPaths
	}

	var added []string

	for path, incomingItem := range incoming.Paths {
		existingPathRaw, pathExists := existingPaths[path]

		if !pathExists {
			// Whole path is new — serialize the entire path item.
			itemMap, err := operationToMap(incomingItem)
			if err != nil {
				continue
			}
			existingPaths[path] = itemMap
			// Collect all methods as added.
			for _, method := range openAPIMethods(incomingItem) {
				added = append(added, strings.ToUpper(method)+" "+path)
			}
			continue
		}

		// Path exists — merge only new methods.
		existingPathMap, ok := existingPathRaw.(map[string]interface{})
		if !ok {
			continue
		}

		methods := map[string]*OpenAPIOperation{
			"get":     incomingItem.Get,
			"post":    incomingItem.Post,
			"put":     incomingItem.Put,
			"patch":   incomingItem.Patch,
			"delete":  incomingItem.Delete,
			"head":    incomingItem.Head,
			"options": incomingItem.Options,
		}

		for method, op := range methods {
			if op == nil {
				continue
			}
			if _, alreadyExists := existingPathMap[method]; alreadyExists {
				continue
			}
			opMap, err := opToMap(op)
			if err != nil {
				continue
			}
			existingPathMap[method] = opMap
			added = append(added, strings.ToUpper(method)+" "+path)
		}
	}

	return added
}

// SerializeMergedYAML serializes the merged map back to YAML bytes.
func SerializeMergedYAML(data map[string]interface{}) ([]byte, error) {
	b, err := yaml.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("openapi merge: serialize yaml: %w", err)
	}
	return b, nil
}

// SerializeMergedJSON serializes the merged map back to indented JSON bytes.
func SerializeMergedJSON(data map[string]interface{}) ([]byte, error) {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("openapi merge: serialize json: %w", err)
	}
	return b, nil
}

// operationToMap converts an OpenAPIPathItem to map[string]interface{} via
// JSON round-trip.
func operationToMap(item OpenAPIPathItem) (map[string]interface{}, error) {
	b, err := json.Marshal(item)
	if err != nil {
		return nil, err
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// opToMap converts a single *OpenAPIOperation to map[string]interface{} via
// JSON round-trip.
func opToMap(op *OpenAPIOperation) (map[string]interface{}, error) {
	b, err := json.Marshal(op)
	if err != nil {
		return nil, err
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// openAPIMethods returns method strings present in the path item.
func openAPIMethods(item OpenAPIPathItem) []string {
	var methods []string
	if item.Get != nil {
		methods = append(methods, "get")
	}
	if item.Post != nil {
		methods = append(methods, "post")
	}
	if item.Put != nil {
		methods = append(methods, "put")
	}
	if item.Patch != nil {
		methods = append(methods, "patch")
	}
	if item.Delete != nil {
		methods = append(methods, "delete")
	}
	if item.Head != nil {
		methods = append(methods, "head")
	}
	if item.Options != nil {
		methods = append(methods, "options")
	}
	return methods
}
