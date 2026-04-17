package export

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// ExistingCollection holds a parsed Postman collection with items kept as raw
// JSON bytes so that unknown fields (scripts, auth, events) are preserved verbatim.
type ExistingCollection struct {
	Info  PostmanInfo
	Items []json.RawMessage // each item is the original JSON bytes
}

// PostmanConflict holds both sides of an item that exists in both collections
// but with a differing request body.
type PostmanConflict struct {
	Key         string
	ExistingRaw json.RawMessage // original bytes — used for "keep" / "merge"
	Existing    PostmanItem     // decoded — used for display
	Incoming    PostmanItem     // incoming version — used for "replace"
}

// MergeResult is the diff of an existing collection against a freshly generated one.
type MergeResult struct {
	Added     []PostmanItem    // in incoming, not in existing
	Conflicts []PostmanConflict
	Unchanged []string         // keys present in both with identical body (no action needed)
}

var postmanParamRe = regexp.MustCompile(`\{\{([^}]+)\}\}`)

// normalizePostmanURL converts a Postman raw URL to a probe path pattern.
// "{{baseUrl}}/api/users/{{id}}" → "/api/users/{id}"
func normalizePostmanURL(raw string) string {
	// Strip {{baseUrl}} prefix.
	raw = strings.TrimPrefix(raw, "{{baseUrl}}")
	// Replace {{param}} with {param}, but skip {{baseUrl}} (already stripped).
	return postmanParamRe.ReplaceAllString(raw, "{$1}")
}

// incomingItemKey returns the normalized key for a PostmanItem from the incoming collection.
func incomingItemKey(item PostmanItem) string {
	path := normalizePostmanURL(item.Request.URL.Raw)
	return strings.ToUpper(item.Request.Method) + " " + path
}

// ExistingItemKey extracts the normalized "METHOD /path/{param}" key from a raw
// Postman item JSON. Also returns the item name for display purposes.
func ExistingItemKey(raw json.RawMessage) (key, name string, err error) {
	var stub struct {
		Name    string `json:"name"`
		Request struct {
			Method string `json:"method"`
			URL    struct {
				Raw string `json:"raw"`
			} `json:"url"`
		} `json:"request"`
	}
	if err := json.Unmarshal(raw, &stub); err != nil {
		return "", "", fmt.Errorf("postman merge: parse item: %w", err)
	}
	path := normalizePostmanURL(stub.Request.URL.Raw)
	key = strings.ToUpper(stub.Request.Method) + " " + path
	return key, stub.Name, nil
}

// decodePostmanItem decodes a raw item into a PostmanItem for diff purposes.
func decodePostmanItem(raw json.RawMessage) (PostmanItem, error) {
	var item PostmanItem
	if err := json.Unmarshal(raw, &item); err != nil {
		return PostmanItem{}, fmt.Errorf("postman merge: decode item: %w", err)
	}
	return item, nil
}

// bodyRaw extracts the raw body string from a PostmanItem for comparison.
func bodyRaw(item PostmanItem) string {
	if item.Request.Body == nil {
		return ""
	}
	return item.Request.Body.Raw
}

// LoadExistingCollection reads a Postman collection JSON file, preserving raw
// item bytes verbatim for safe round-tripping.
func LoadExistingCollection(path string) (*ExistingCollection, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("postman merge: read %s: %w", path, err)
	}

	var wrapper struct {
		Info  PostmanInfo       `json:"info"`
		Items []json.RawMessage `json:"item"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Errorf("postman merge: parse collection: %w", err)
	}

	return &ExistingCollection{
		Info:  wrapper.Info,
		Items: wrapper.Items,
	}, nil
}

// ComputeMerge diffs an existing collection against a freshly generated one.
// Returns the sets of added items, conflicts, and unchanged keys.
func ComputeMerge(existing *ExistingCollection, incoming *PostmanCollection) (MergeResult, error) {
	// Build a key→raw index for the existing collection.
	existingIndex := make(map[string]json.RawMessage, len(existing.Items))
	for _, raw := range existing.Items {
		key, _, err := ExistingItemKey(raw)
		if err != nil {
			return MergeResult{}, err
		}
		existingIndex[key] = raw
	}

	var result MergeResult

	for _, inc := range incoming.Item {
		key := incomingItemKey(inc)
		existRaw, found := existingIndex[key]
		if !found {
			result.Added = append(result.Added, inc)
			continue
		}

		existItem, err := decodePostmanItem(existRaw)
		if err != nil {
			return MergeResult{}, err
		}

		if bodyRaw(existItem) == bodyRaw(inc) {
			result.Unchanged = append(result.Unchanged, key)
		} else {
			result.Conflicts = append(result.Conflicts, PostmanConflict{
				Key:         key,
				ExistingRaw: existRaw,
				Existing:    existItem,
				Incoming:    inc,
			})
		}
	}

	return result, nil
}

// BuildMergedCollection assembles the final collection JSON with resolutions applied.
// resolutions maps item key → "keep" | "replace" | "merge" (field-level merge of body).
// Conflicts with no resolution in the map default to "keep".
// Items in existing not in incoming are always preserved.
// New items are appended after the existing ones.
func BuildMergedCollection(existing *ExistingCollection, incoming *PostmanCollection, result MergeResult, resolutions map[string]string) ([]byte, error) {
	// Build conflict index for quick lookup.
	conflictIndex := make(map[string]PostmanConflict, len(result.Conflicts))
	for _, c := range result.Conflicts {
		conflictIndex[c.Key] = c
	}

	// Build incoming index for quick lookup.
	incomingIndex := make(map[string]PostmanItem, len(incoming.Item))
	for _, item := range incoming.Item {
		incomingIndex[incomingItemKey(item)] = item
	}

	// Process existing items in order.
	var items []json.RawMessage
	for _, raw := range existing.Items {
		key, _, err := ExistingItemKey(raw)
		if err != nil {
			return nil, err
		}

		conflict, isConflict := conflictIndex[key]
		if !isConflict {
			// Not a conflict — preserve verbatim.
			items = append(items, raw)
			continue
		}

		resolution := "keep"
		if r, ok := resolutions[key]; ok {
			resolution = r
		}

		switch resolution {
		case "replace":
			b, err := json.Marshal(conflict.Incoming)
			if err != nil {
				return nil, fmt.Errorf("postman merge: marshal replaced item: %w", err)
			}
			items = append(items, b)

		case "merge":
			merged, err := mergeItemBody(raw, conflict.Incoming)
			if err != nil {
				return nil, err
			}
			items = append(items, merged)

		default: // "keep"
			items = append(items, raw)
		}
	}

	// Append new items.
	for _, added := range result.Added {
		b, err := json.Marshal(added)
		if err != nil {
			return nil, fmt.Errorf("postman merge: marshal added item: %w", err)
		}
		items = append(items, b)
	}

	// Assemble the final collection preserving the original info block.
	info := existing.Info
	if info.Schema == "" {
		info.Schema = postmanSchemaURL
	}

	out := struct {
		Info  PostmanInfo       `json:"info"`
		Items []json.RawMessage `json:"item"`
	}{
		Info:  info,
		Items: items,
	}

	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("postman merge: marshal output: %w", err)
	}
	return b, nil
}

// mergeItemBody adds missing top-level body fields from incoming into the existing
// item's raw JSON, without touching any other fields on the item.
func mergeItemBody(existingRaw json.RawMessage, incoming PostmanItem) (json.RawMessage, error) {
	// Decode existing as a generic map to preserve all fields.
	var existMap map[string]json.RawMessage
	if err := json.Unmarshal(existingRaw, &existMap); err != nil {
		return existingRaw, fmt.Errorf("postman merge: decode existing item map: %w", err)
	}

	// If incoming has no body, nothing to merge.
	if incoming.Request.Body == nil || incoming.Request.Body.Raw == "" {
		return existingRaw, nil
	}

	// Decode the existing body JSON (if present).
	var existBodyFields map[string]json.RawMessage
	if reqRaw, ok := existMap["request"]; ok {
		var reqMap map[string]json.RawMessage
		if err := json.Unmarshal(reqRaw, &reqMap); err == nil {
			if bodyRaw, ok := reqMap["body"]; ok {
				var bodyMap map[string]json.RawMessage
				if err := json.Unmarshal(bodyRaw, &bodyMap); err == nil {
					if rawField, ok := bodyMap["raw"]; ok {
						var bodyStr string
						if err := json.Unmarshal(rawField, &bodyStr); err == nil {
							_ = json.Unmarshal([]byte(bodyStr), &existBodyFields)
						}
					}
				}
			}
		}
	}
	if existBodyFields == nil {
		existBodyFields = make(map[string]json.RawMessage)
	}

	// Add missing fields from incoming body.
	var incomingBodyFields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(incoming.Request.Body.Raw), &incomingBodyFields); err != nil {
		// Incoming body is not JSON — fall back to keep.
		return existingRaw, nil
	}
	changed := false
	for k, v := range incomingBodyFields {
		if _, exists := existBodyFields[k]; !exists {
			existBodyFields[k] = v
			changed = true
		}
	}
	if !changed {
		return existingRaw, nil
	}

	// Re-serialize the merged body.
	mergedBody, err := json.Marshal(existBodyFields)
	if err != nil {
		return existingRaw, fmt.Errorf("postman merge: marshal merged body: %w", err)
	}
	mergedBodyStr, _ := json.Marshal(string(mergedBody))

	// Patch the raw body string in the existing item's request.body.raw.
	var reqMap map[string]json.RawMessage
	if err := json.Unmarshal(existMap["request"], &reqMap); err != nil {
		return existingRaw, nil
	}
	var bodyMap map[string]json.RawMessage
	if err := json.Unmarshal(reqMap["body"], &bodyMap); err != nil {
		return existingRaw, nil
	}
	bodyMap["raw"] = mergedBodyStr
	newBodyJSON, _ := json.Marshal(bodyMap)
	reqMap["body"] = newBodyJSON
	newReqJSON, _ := json.Marshal(reqMap)
	existMap["request"] = newReqJSON

	result, err := json.Marshal(existMap)
	if err != nil {
		return existingRaw, fmt.Errorf("postman merge: marshal merged item: %w", err)
	}
	return result, nil
}
