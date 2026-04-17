package cmd

import (
	"flag"
	"fmt"
	"os"

	"github.com/AgusRdz/probe/config"
	"github.com/AgusRdz/probe/export"
	"github.com/AgusRdz/probe/store"
)

// RunExport runs `probe export [flags]`.
func RunExport(args []string, cfg *config.Config) {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: probe export [flags]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "flags:")
		fs.PrintDefaults()
	}

	format := fs.String("format", cfg.Export.DefaultFormat, `output format: "openapi" (YAML, default), "json" (OpenAPI as JSON), "postman"`)
	out := fs.String("out", "", "output file path (default: stdout)")
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
		Format:    *format,
		MinCalls:  *minCalls,
		InfoTitle: cfg.Export.InfoTitle,
		InfoVersion: cfg.Export.InfoVersion,
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

	if *format == "postman" {
		col, err := export.GeneratePostman(s, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "probe: generate postman: %v\n", err)
			os.Exit(1)
		}
		if err := export.WritePostman(w, col); err != nil {
			fmt.Fprintf(os.Stderr, "probe: write postman: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "\nExported %d endpoints to Postman collection.\n\n", len(col.Item))
		return
	}

	spec, count, err := export.GenerateOpenAPI(s, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "probe: generate openapi: %v\n", err)
		os.Exit(1)
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
