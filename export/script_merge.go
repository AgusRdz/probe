package export

import (
	"bytes"
	"regexp"
	"strings"
)

var scriptSeparatorRe = regexp.MustCompile(`(?m)^# -{3,}`)

// ParseScriptEndpointKeys extracts all "METHOD /path" keys from a
// probe-generated curl or HTTPie script.
// Looks for the comment pattern between separator lines:
//
//	# ---------------------------------------------------------------------------
//	# METHOD /path
//	# ---------------------------------------------------------------------------
func ParseScriptEndpointKeys(script []byte) map[string]bool {
	keys := make(map[string]bool)
	lines := strings.Split(string(script), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "# ---") {
			continue
		}
		// Next non-empty line should be "# METHOD /path".
		if i+1 >= len(lines) {
			continue
		}
		candidate := strings.TrimSpace(lines[i+1])
		if !strings.HasPrefix(candidate, "# ") {
			continue
		}
		// Verify followed by another separator.
		if i+2 >= len(lines) {
			continue
		}
		following := strings.TrimSpace(lines[i+2])
		if !strings.HasPrefix(following, "# ---") {
			continue
		}
		key := strings.TrimPrefix(candidate, "# ")
		// Validate it looks like "METHOD /path".
		parts := strings.SplitN(key, " ", 2)
		if len(parts) == 2 && strings.HasPrefix(parts[1], "/") {
			keys[key] = true
		}
	}
	return keys
}

// MergeScript appends endpoint blocks from incoming that are not already in
// existing. Returns the merged bytes and the list of added "METHOD /path" keys.
func MergeScript(existing, incoming []byte) (merged []byte, added []string) {
	existingKeys := ParseScriptEndpointKeys(existing)

	// Split incoming into header + endpoint blocks.
	// Each block starts with "\n# ---" separator.
	blocks := splitScriptBlocks(incoming)

	// Start with existing content.
	result := make([]byte, len(existing))
	copy(result, existing)
	// Trim trailing newline to avoid extra blank lines.
	result = bytes.TrimRight(result, "\n")

	for _, block := range blocks {
		key := extractBlockKey(block)
		if key == "" || existingKeys[key] {
			continue
		}
		// Ensure the block starts with a newline separator.
		toAppend := block
		if !bytes.HasPrefix(toAppend, []byte("\n")) {
			toAppend = append([]byte("\n"), toAppend...)
		}
		result = append(result, toAppend...)
		added = append(added, key)
	}

	// Ensure trailing newline.
	if len(result) > 0 && result[len(result)-1] != '\n' {
		result = append(result, '\n')
	}

	return result, added
}

// splitScriptBlocks splits a script into endpoint blocks. Each block is the
// content starting from "\n# ---" up to (but not including) the next one.
func splitScriptBlocks(script []byte) [][]byte {
	// Find all positions of "\n# ---" in the script.
	sep := []byte("\n# ---")
	var positions []int
	start := 0
	for {
		idx := bytes.Index(script[start:], sep)
		if idx < 0 {
			break
		}
		abs := start + idx
		positions = append(positions, abs)
		start = abs + 1
	}

	if len(positions) == 0 {
		return nil
	}

	var blocks [][]byte
	for i, pos := range positions {
		var end int
		if i+1 < len(positions) {
			end = positions[i+1]
		} else {
			end = len(script)
		}
		block := script[pos:end]
		// Only include blocks that have a METHOD /path header.
		if extractBlockKey(block) != "" {
			blocks = append(blocks, block)
		}
	}
	return blocks
}

// extractBlockKey returns the "METHOD /path" key from a block, or "" if the
// block doesn't match the expected separator+header pattern.
// The closing separator is the start of the NEXT block, so we only require:
// a separator line followed by a "# METHOD /path" line.
func extractBlockKey(block []byte) string {
	lines := strings.Split(string(block), "\n")
	// Find the first separator line.
	sepIdx := -1
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "# ---") {
			sepIdx = i
			break
		}
	}
	if sepIdx < 0 || sepIdx+1 >= len(lines) {
		return ""
	}
	headerLine := strings.TrimSpace(lines[sepIdx+1])
	if !strings.HasPrefix(headerLine, "# ") {
		return ""
	}
	key := strings.TrimPrefix(headerLine, "# ")
	parts := strings.SplitN(key, " ", 2)
	if len(parts) == 2 && strings.HasPrefix(parts[1], "/") {
		return key
	}
	return ""
}
