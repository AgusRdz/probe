package export

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

// BrunoConflict holds both sides of a .bru file that exists in the directory
// but with different content than the incoming version.
type BrunoConflict struct {
	Filename string
	Key      string // equals Filename for Bruno (no path reversal needed)
	Existing []byte
	Incoming []byte
}

// ComputeBrunoMerge returns the filenames from incoming BrunoCollection that do
// NOT already exist in dir. Skips "bruno.json" (manifest — never overwritten).
// Returns added (filenames to write) and conflicts (filenames that exist with
// different content).
func ComputeBrunoMerge(dir string, incoming BrunoCollection) (added []string, conflicts []BrunoConflict, err error) {
	for name, incomingContent := range incoming {
		// Never overwrite the manifest.
		if name == "bruno.json" {
			continue
		}

		existPath := filepath.Join(dir, name)
		existContent, readErr := os.ReadFile(existPath)
		if readErr != nil {
			// File does not exist — it's new.
			added = append(added, name)
			continue
		}

		if bytes.Equal(existContent, incomingContent) {
			// Identical — nothing to do.
			continue
		}

		// Different content — conflict.
		conflicts = append(conflicts, BrunoConflict{
			Filename: name,
			Key:      name,
			Existing: existContent,
			Incoming: incomingContent,
		})
	}
	return added, conflicts, nil
}

// ApplyBrunoMerge writes files from incoming for the given filenames into dir.
func ApplyBrunoMerge(dir string, incoming BrunoCollection, toWrite []string) error {
	for _, name := range toWrite {
		content, ok := incoming[name]
		if !ok {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			return fmt.Errorf("bruno merge: write %s: %w", name, err)
		}
	}
	return nil
}
