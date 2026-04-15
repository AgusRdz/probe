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

	format := fs.String("format", cfg.Export.DefaultFormat, `output format: "openapi" (default) or "postman"`)
	out := fs.String("out", "", "output file path (default: stdout)")
	minConfidence := fs.Float64("min-confidence", cfg.Export.MinConfidence, "minimum confidence threshold (0.0–1.0)")
	includeSkeleton := fs.Bool("include-skeleton", cfg.Export.IncludeSkeleton, "include scan-only endpoints with 0 calls")
	includeUnconfirmed := fs.Bool("include-unconfirmed", false, "include endpoints with unconfirmed path patterns")
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
		Format:             *format,
		MinConfidence:      *minConfidence,
		IncludeSkeleton:    *includeSkeleton,
		IncludeUnconfirmed: *includeUnconfirmed,
		InfoTitle:          cfg.Export.InfoTitle,
		InfoVersion:        cfg.Export.InfoVersion,
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
		return
	}

	spec, err := export.GenerateOpenAPI(s, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "probe: generate openapi: %v\n", err)
		os.Exit(1)
	}

	if err := export.WriteYAML(w, spec); err != nil {
		fmt.Fprintf(os.Stderr, "probe: write yaml: %v\n", err)
		os.Exit(1)
	}
}
