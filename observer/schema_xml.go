package observer

import (
	"bytes"
	"encoding/xml"
	"io"
)

// InferXMLBody parses raw XML bytes and returns an inferred Schema.
// Handles: elements, attributes, text content, nested elements.
// Returns nil if bytes are empty or not valid XML.
// Attribute names are prefixed with "@" (e.g., "@id").
// Nested elements become nested object schemas.
// Text-only elements get type "string".
// Values are NEVER stored — only type metadata.
func InferXMLBody(data []byte) *Schema {
	if len(data) == 0 {
		return nil
	}

	dec := xml.NewDecoder(bytes.NewReader(data))

	// Advance past the XML declaration / processing instructions to the root element.
	var root *xmlNode
	var stack []*xmlNode

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil
		}

		switch t := tok.(type) {
		case xml.StartElement:
			node := &xmlNode{name: t.Name.Local}
			for _, attr := range t.Attr {
				node.attrs = append(node.attrs, attr.Name.Local)
			}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, node)
			} else {
				root = node
			}
			stack = append(stack, node)

		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}

		case xml.CharData:
			if len(stack) > 0 {
				text := string(bytes.TrimSpace([]byte(t)))
				if text != "" {
					stack[len(stack)-1].hasText = true
				}
			}
		}
	}

	if root == nil {
		return nil
	}

	s := xmlNodeToSchema(root)
	return &s
}

// xmlNode is a lightweight intermediate tree used during XML parsing.
type xmlNode struct {
	name     string
	attrs    []string
	children []*xmlNode
	hasText  bool
}

// xmlNodeToSchema converts an xmlNode tree into a Schema.
// Attributes become "@name" string properties.
// Child elements become nested object properties.
// Text-only nodes become type "string".
func xmlNodeToSchema(node *xmlNode) Schema {
	if len(node.children) == 0 && len(node.attrs) == 0 {
		// Leaf node — text content or empty.
		return Schema{Type: "string"}
	}

	props := make(map[string]*Schema)

	// Attributes → "@name" string properties.
	for _, attr := range node.attrs {
		s := Schema{Type: "string", XMLAttr: true}
		props["@"+attr] = &s
	}

	// Child elements → nested schemas.
	// If a child name appears multiple times, wrap as array.
	childCount := make(map[string]int)
	for _, child := range node.children {
		childCount[child.name]++
	}

	seen := make(map[string]bool)
	for _, child := range node.children {
		if seen[child.name] {
			continue
		}
		seen[child.name] = true

		childSchema := xmlNodeToSchema(child)
		if childCount[child.name] > 1 {
			// Repeated element → array.
			itemCopy := childSchema
			props[child.name] = &Schema{Type: "array", Items: &itemCopy}
		} else {
			cp := childSchema
			props[child.name] = &cp
		}
	}

	// If node has both text and children, add a "_text" string field.
	if node.hasText && len(node.children) > 0 {
		s := Schema{Type: "string"}
		props["_text"] = &s
	}

	return Schema{Type: "object", Properties: props}
}
