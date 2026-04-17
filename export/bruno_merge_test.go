package export

import (
	"os"
	"path/filepath"
	"testing"
)

// TestComputeBrunoMerge_AllNew verifies that when no .bru files exist in the
// directory, all incoming files are reported as added.
func TestComputeBrunoMerge_AllNew(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	incoming := BrunoCollection{
		"bruno.json":       []byte(`{"version":"1"}`),
		"get-api-users.bru": []byte("meta { name: GET /api/users }"),
		"post-api-users.bru": []byte("meta { name: POST /api/users }"),
	}
	added, conflicts, err := ComputeBrunoMerge(dir, incoming)
	if err != nil {
		t.Fatalf("ComputeBrunoMerge: %v", err)
	}
	// bruno.json is never added.
	if len(added) != 2 {
		t.Errorf("expected 2 added; got %d: %v", len(added), added)
	}
	if len(conflicts) != 0 {
		t.Errorf("expected 0 conflicts; got %d", len(conflicts))
	}
}

// TestComputeBrunoMerge_SomeExist verifies that existing files with the same
// content are skipped and only missing files are added.
func TestComputeBrunoMerge_SomeExist(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Pre-write one file.
	existContent := []byte("meta { name: GET /api/users }")
	if err := os.WriteFile(filepath.Join(dir, "get-api-users.bru"), existContent, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	incoming := BrunoCollection{
		"bruno.json":         []byte(`{"version":"1"}`),
		"get-api-users.bru":  existContent,
		"post-api-users.bru": []byte("meta { name: POST /api/users }"),
	}
	added, conflicts, err := ComputeBrunoMerge(dir, incoming)
	if err != nil {
		t.Fatalf("ComputeBrunoMerge: %v", err)
	}
	if len(added) != 1 {
		t.Errorf("expected 1 added; got %d: %v", len(added), added)
	}
	if added[0] != "post-api-users.bru" {
		t.Errorf("expected post-api-users.bru to be added; got %q", added[0])
	}
	if len(conflicts) != 0 {
		t.Errorf("expected 0 conflicts; got %d", len(conflicts))
	}
}

// TestComputeBrunoMerge_Conflict verifies that a file with different content
// is reported as a conflict.
func TestComputeBrunoMerge_Conflict(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "get-api-users.bru"), []byte("old content"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	incoming := BrunoCollection{
		"get-api-users.bru": []byte("new content"),
	}
	added, conflicts, err := ComputeBrunoMerge(dir, incoming)
	if err != nil {
		t.Fatalf("ComputeBrunoMerge: %v", err)
	}
	if len(added) != 0 {
		t.Errorf("expected 0 added; got %d", len(added))
	}
	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict; got %d", len(conflicts))
	}
	if conflicts[0].Filename != "get-api-users.bru" {
		t.Errorf("conflict filename: got %q; want get-api-users.bru", conflicts[0].Filename)
	}
}

// TestApplyBrunoMerge verifies that ApplyBrunoMerge writes the specified files.
func TestApplyBrunoMerge(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	incoming := BrunoCollection{
		"get-api-users.bru":  []byte("meta { name: GET /api/users }"),
		"post-api-users.bru": []byte("meta { name: POST /api/users }"),
	}
	toWrite := []string{"get-api-users.bru", "post-api-users.bru"}
	if err := ApplyBrunoMerge(dir, incoming, toWrite); err != nil {
		t.Fatalf("ApplyBrunoMerge: %v", err)
	}
	for _, name := range toWrite {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Errorf("ReadFile %s: %v", name, err)
			continue
		}
		if string(b) != string(incoming[name]) {
			t.Errorf("%s content mismatch", name)
		}
	}
}
