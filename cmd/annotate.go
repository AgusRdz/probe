package cmd

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/AgusRdz/probe/config"
	"github.com/AgusRdz/probe/store"
)

// multiFlag is a repeatable string flag (e.g. --tag users --tag admin).
type multiFlag []string

func (f *multiFlag) String() string  { return strings.Join(*f, ", ") }
func (f *multiFlag) Set(v string) error { *f = append(*f, v); return nil }

// RunAnnotate runs `probe annotate "METHOD /path" [flags]`.
func RunAnnotate(args []string, cfg *config.Config) {
	fs := flag.NewFlagSet("annotate", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, `usage: probe annotate "METHOD /path" [flags]`)
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "flags:")
		fs.PrintDefaults()
	}

	description := fs.String("description", "", "human-readable description for the endpoint")
	var tags multiFlag
	fs.Var(&tags, "tag", "tag to apply (repeatable)")
	pathOverride := fs.String("path-override", "", "canonical path pattern override")
	db := fs.String("db", "", "override DB path")

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Fprintln(os.Stderr, `probe annotate: "METHOD /path" argument is required`)
		fs.Usage()
		os.Exit(1)
	}

	// Positional: first arg may be "GET /users" or provided as two args.
	var method, path string
	switch len(rest) {
	case 1:
		parts := strings.SplitN(rest[0], " ", 2)
		if len(parts) != 2 {
			fmt.Fprintf(os.Stderr, "probe annotate: cannot parse %q — expected \"METHOD PATH\"\n", rest[0])
			os.Exit(1)
		}
		method = strings.ToUpper(parts[0])
		path = parts[1]
	default:
		method = strings.ToUpper(rest[0])
		path = rest[1]
	}

	s, err := store.Open(*db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "probe: open store: %v\n", err)
		os.Exit(1)
	}
	defer s.Close() //nolint:errcheck

	endpoints, err := s.GetEndpoints()
	if err != nil {
		fmt.Fprintf(os.Stderr, "probe: get endpoints: %v\n", err)
		os.Exit(1)
	}

	var found *store.Endpoint
	for i := range endpoints {
		if strings.EqualFold(endpoints[i].Method, method) && endpoints[i].PathPattern == path {
			found = &endpoints[i]
			break
		}
	}

	if found == nil {
		fmt.Fprintf(os.Stderr, "probe annotate: endpoint not found: %s %s\n", method, path)
		os.Exit(1)
	}

	// If no flags provided, open $EDITOR with current values.
	if *description == "" && len(tags) == 0 && *pathOverride == "" {
		edited, err := openEditor(found.Description, found.Tags)
		if err != nil {
			fmt.Fprintf(os.Stderr, "probe annotate: editor: %v\n", err)
			os.Exit(1)
		}
		*description = edited.description
		tags = edited.tags
	}

	// cfg accepted for future use.
	_ = cfg

	var tagsSlice []string
	if len(tags) > 0 {
		tagsSlice = []string(tags)
	}

	if err := s.UpdateEndpointAnnotation(found.ID, *description, tagsSlice); err != nil {
		fmt.Fprintf(os.Stderr, "probe: update annotation: %v\n", err)
		os.Exit(1)
	}

	if *pathOverride != "" {
		if err := s.UpsertPathOverride(method, path, *pathOverride); err != nil {
			fmt.Fprintf(os.Stderr, "probe: upsert path override: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Printf("probe: annotated %s %s\n", method, path)
}

// editorResult holds values parsed back from the temp file.
type editorResult struct {
	description string
	tags        []string
}

// openEditor writes current values to a temp file, opens $EDITOR, and reads
// back the result. Falls back to $VISUAL, then "vi".
func openEditor(currentDesc string, currentTags []string) (editorResult, error) {
	f, err := os.CreateTemp("", "probe-annotate-*.txt")
	if err != nil {
		return editorResult{}, fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(f.Name())

	tagsLine := strings.Join(currentTags, ", ")
	content := fmt.Sprintf("description: %s\ntags: %s\n", currentDesc, tagsLine)
	if _, err := fmt.Fprint(f, content); err != nil {
		f.Close()
		return editorResult{}, fmt.Errorf("write temp file: %w", err)
	}
	f.Close()

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "vi"
	}

	cmd := exec.Command(editor, f.Name())
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return editorResult{}, fmt.Errorf("editor exited with error: %w", err)
	}

	data, err := os.ReadFile(f.Name())
	if err != nil {
		return editorResult{}, fmt.Errorf("read temp file: %w", err)
	}

	return parseEditorOutput(string(data)), nil
}

// parseEditorOutput extracts description and tags from a simple "key: value" format.
func parseEditorOutput(raw string) editorResult {
	var result editorResult
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "description:") {
			result.description = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
		} else if strings.HasPrefix(line, "tags:") {
			tagStr := strings.TrimSpace(strings.TrimPrefix(line, "tags:"))
			if tagStr != "" {
				parts := strings.Split(tagStr, ",")
				for _, p := range parts {
					p = strings.TrimSpace(p)
					if p != "" {
						result.tags = append(result.tags, p)
					}
				}
			}
		}
	}
	return result
}
