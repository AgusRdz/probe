package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// PrintJSON writes v as an indented JSON value to w.
// indent controls the number of spaces per indentation level.
func PrintJSON(w io.Writer, v any, indent int) error {
	prefix := ""
	indentStr := strings.Repeat(" ", indent)
	b, err := json.MarshalIndent(v, prefix, indentStr)
	if err != nil {
		return fmt.Errorf("render: marshal json: %w", err)
	}
	_, err = fmt.Fprintf(w, "%s\n", b)
	return err
}
