package cmd

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/AgusRdz/probe/config"
	"github.com/AgusRdz/probe/export"
	"github.com/AgusRdz/probe/store"
)

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// RunExport runs `probe export [flags]`.
func RunExport(args []string, cfg *config.Config) {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: probe export [flags]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "flags:")
		fs.PrintDefaults()
	}

	format := fs.String("format", cfg.Export.DefaultFormat, `output format: openapi (YAML), json (OpenAPI JSON), swagger (2.0), postman, curl, httpie, bruno`)
	out := fs.String("out", "", "output file or directory path (default: stdout; for bruno: ./<title>-bruno/)")
	minCalls := fs.Int("min-calls", cfg.Export.MinCalls, "only export endpoints with at least N traffic calls (0 = include scan-only endpoints too)")
	db := fs.String("db", "", "override DB path")

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	s, err := store.Open(*db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "probe: open store: %v\n", err)
		os.Exit(1)
	}
	defer s.Close() //nolint:errcheck

	opts := export.ExportOptions{
		Format:      *format,
		MinCalls:    *minCalls,
		InfoTitle:   cfg.Export.InfoTitle,
		InfoVersion: cfg.Export.InfoVersion,
	}

	switch *format {
	case "curl":
		data, err := export.GenerateCurl(s, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "probe: generate curl: %v\n", err)
			os.Exit(1)
		}
		if err := writeBytes(data, *out); err != nil {
			fmt.Fprintf(os.Stderr, "probe: write curl: %v\n", err)
			os.Exit(1)
		}
		return

	case "httpie":
		data, err := export.GenerateHTTPie(s, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "probe: generate httpie: %v\n", err)
			os.Exit(1)
		}
		if err := writeBytes(data, *out); err != nil {
			fmt.Fprintf(os.Stderr, "probe: write httpie: %v\n", err)
			os.Exit(1)
		}
		return

	case "swagger":
		spec, err := export.GenerateSwagger(s, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "probe: generate swagger: %v\n", err)
			os.Exit(1)
		}
		w := os.Stdout
		if *out != "" {
			f, err := os.Create(*out)
			if err != nil {
				fmt.Fprintf(os.Stderr, "probe: create output file: %v\n", err)
				os.Exit(1)
			}
			defer f.Close() //nolint:errcheck
			w = f
		}
		if err := export.WriteSwaggerYAML(w, spec); err != nil {
			fmt.Fprintf(os.Stderr, "probe: write swagger: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "\nExported Swagger 2.0 spec")
		if *out != "" {
			fmt.Fprintf(os.Stderr, " → %s", *out)
		}
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr)
		return

	case "bruno":
		collection, err := export.GenerateBruno(s, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "probe: generate bruno: %v\n", err)
			os.Exit(1)
		}
		dir := *out
		if dir == "" {
			title := opts.InfoTitle
			if title == "" {
				title = "api"
			}
			slug := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(title), "-"), "-")
			dir = "./" + slug + "-bruno"
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "probe: create bruno dir: %v\n", err)
			os.Exit(1)
		}
		for name, content := range collection {
			path := filepath.Join(dir, name)
			if err := os.WriteFile(path, content, 0o644); err != nil {
				fmt.Fprintf(os.Stderr, "probe: write bruno file %s: %v\n", name, err)
				os.Exit(1)
			}
		}
		// Count requests (exclude bruno.json manifest).
		requestCount := len(collection) - 1
		if requestCount < 0 {
			requestCount = 0
		}
		fmt.Fprintf(os.Stderr, "\nCreated Bruno collection: %s/ (%d requests)\n\n", dir, requestCount)
		return

	case "postman":
		col, err := export.GeneratePostman(s, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "probe: generate postman: %v\n", err)
			os.Exit(1)
		}
		w := os.Stdout
		if *out != "" {
			f, err := os.Create(*out)
			if err != nil {
				fmt.Fprintf(os.Stderr, "probe: create output file: %v\n", err)
				os.Exit(1)
			}
			defer f.Close() //nolint:errcheck
			w = f
		}
		if err := export.WritePostman(w, col); err != nil {
			fmt.Fprintf(os.Stderr, "probe: write postman: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "\nExported %d endpoints to Postman collection.\n\n", len(col.Item))
		return
	}

	// Default: openapi (YAML or JSON).
	spec, count, err := export.GenerateOpenAPI(s, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "probe: generate openapi: %v\n", err)
		os.Exit(1)
	}

	w := os.Stdout
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			fmt.Fprintf(os.Stderr, "probe: create output file: %v\n", err)
			os.Exit(1)
		}
		defer f.Close() //nolint:errcheck
		w = f
	}

	if *format == "json" {
		if err := export.WriteJSON(w, spec); err != nil {
			fmt.Fprintf(os.Stderr, "probe: write json: %v\n", err)
			os.Exit(1)
		}
	} else {
		if err := export.WriteYAML(w, spec); err != nil {
			fmt.Fprintf(os.Stderr, "probe: write yaml: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Fprintf(os.Stderr, "\nExported %d endpoints", count)
	if *out != "" {
		fmt.Fprintf(os.Stderr, " → %s", *out)
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr)
}

// writeBytes writes data to path, or to stdout if path is empty.
func writeBytes(data []byte, path string) error {
	if path == "" {
		_, err := os.Stdout.Write(data)
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
