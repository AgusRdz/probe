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

// formatExtensions maps format names to their default file extension.
var formatExtensions = map[string]string{
	"openapi": ".yaml",
	"json":    ".json",
	"swagger": ".swagger.yaml",
	"postman": ".postman_collection.json",
	"curl":    ".sh",
	"httpie":  ".httpie.sh",
	"bruno":   "-bruno", // directory suffix
}

// RunExport runs `probe export [flags]`.
func RunExport(args []string, cfg *config.Config) {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: probe export [flags]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "flags:")
		fs.PrintDefaults()
	}

	// General flags.
	format   := fs.String("format", cfg.Export.DefaultFormat, "output format: openapi (YAML), json (OpenAPI JSON), swagger (2.0), postman, curl, httpie, bruno")
	out      := fs.String("out", "", "output file or directory path (overrides config and smart default)")
	minCalls := fs.Int("min-calls", cfg.Export.MinCalls, "only export endpoints with at least N traffic calls (0 = include scan-only too)")
	db       := fs.String("db", "", "override DB path")

	// Shorthand flags — each selects a format and enables smart default output naming.
	fOpenAPI  := fs.Bool("openapi",  false, "shorthand for --format openapi  (default output: <dir>.yaml)")
	fJSON     := fs.Bool("json",     false, "shorthand for --format json     (default output: <dir>.json)")
	fSwagger  := fs.Bool("swagger",  false, "shorthand for --format swagger  (default output: <dir>.swagger.yaml)")
	fPostman  := fs.Bool("postman",  false, "shorthand for --format postman  (default output: <dir>.postman_collection.json)")
	fCurl     := fs.Bool("curl",     false, "shorthand for --format curl     (default output: <dir>.sh)")
	fHTTPie   := fs.Bool("httpie",   false, "shorthand for --format httpie   (default output: <dir>.httpie.sh)")
	fBruno    := fs.Bool("bruno",    false, "shorthand for --format bruno    (default output: <dir>-bruno/)")

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	// Resolve format: shorthand flags take precedence over --format.
	resolvedFormat := *format
	usingShorthand := false
	for _, sf := range []struct {
		flag bool
		name string
	}{
		{*fPostman, "postman"},
		{*fOpenAPI, "openapi"},
		{*fJSON, "json"},
		{*fSwagger, "swagger"},
		{*fCurl, "curl"},
		{*fHTTPie, "httpie"},
		{*fBruno, "bruno"},
	} {
		if sf.flag {
			resolvedFormat = sf.name
			usingShorthand = true
			break
		}
	}

	// Resolve output path:
	// 1. --out flag (always wins)
	// 2. cfg.Export.Outputs[format] (config default for this format)
	// 3. <cwd-basename><ext> when using a shorthand flag
	// 4. stdout (--format without --out, no config)
	resolvedOut := *out
	if resolvedOut == "" {
		if cfgOut, ok := cfg.Export.Outputs[resolvedFormat]; ok && cfgOut != "" {
			resolvedOut = cfgOut
		} else if usingShorthand {
			cwd, _ := os.Getwd()
			base := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(filepath.Base(cwd)), "-"), "-")
			if base == "" {
				base = "api"
			}
			resolvedOut = base + formatExtensions[resolvedFormat]
		}
	}

	s, err := store.Open(*db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "probe: open store: %v\n", err)
		os.Exit(1)
	}
	defer s.Close() //nolint:errcheck

	opts := export.ExportOptions{
		Format:      resolvedFormat,
		MinCalls:    *minCalls,
		InfoTitle:   cfg.Export.InfoTitle,
		InfoVersion: cfg.Export.InfoVersion,
	}

	switch resolvedFormat {
	case "curl":
		data, err := export.GenerateCurl(s, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "probe: generate curl: %v\n", err)
			os.Exit(1)
		}
		if err := writeBytes(data, resolvedOut); err != nil {
			fmt.Fprintf(os.Stderr, "probe: write curl: %v\n", err)
			os.Exit(1)
		}
		printExportSummary("curl script", resolvedOut)

	case "httpie":
		data, err := export.GenerateHTTPie(s, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "probe: generate httpie: %v\n", err)
			os.Exit(1)
		}
		if err := writeBytes(data, resolvedOut); err != nil {
			fmt.Fprintf(os.Stderr, "probe: write httpie: %v\n", err)
			os.Exit(1)
		}
		printExportSummary("HTTPie script", resolvedOut)

	case "swagger":
		spec, err := export.GenerateSwagger(s, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "probe: generate swagger: %v\n", err)
			os.Exit(1)
		}
		if err := writeToFileOrStdout(resolvedOut, func(w *os.File) error {
			return export.WriteSwaggerYAML(w, spec)
		}); err != nil {
			fmt.Fprintf(os.Stderr, "probe: write swagger: %v\n", err)
			os.Exit(1)
		}
		printExportSummary("Swagger 2.0", resolvedOut)

	case "bruno":
		collection, err := export.GenerateBruno(s, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "probe: generate bruno: %v\n", err)
			os.Exit(1)
		}
		dir := resolvedOut
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
			if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
				fmt.Fprintf(os.Stderr, "probe: write bruno file %s: %v\n", name, err)
				os.Exit(1)
			}
		}
		requestCount := len(collection) - 1
		if requestCount < 0 {
			requestCount = 0
		}
		fmt.Fprintf(os.Stderr, "\nCreated Bruno collection: %s/ (%d requests)\n\n", dir, requestCount)

	case "postman":
		col, err := export.GeneratePostman(s, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "probe: generate postman: %v\n", err)
			os.Exit(1)
		}
		if err := writeToFileOrStdout(resolvedOut, func(w *os.File) error {
			return export.WritePostman(w, col)
		}); err != nil {
			fmt.Fprintf(os.Stderr, "probe: write postman: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "\nExported %d endpoints to Postman collection", len(col.Item))
		if resolvedOut != "" {
			fmt.Fprintf(os.Stderr, " → %s", resolvedOut)
		}
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr)

	default: // openapi (yaml or json)
		spec, count, err := export.GenerateOpenAPI(s, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "probe: generate openapi: %v\n", err)
			os.Exit(1)
		}
		if err := writeToFileOrStdout(resolvedOut, func(w *os.File) error {
			if resolvedFormat == "json" {
				return export.WriteJSON(w, spec)
			}
			return export.WriteYAML(w, spec)
		}); err != nil {
			fmt.Fprintf(os.Stderr, "probe: write openapi: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "\nExported %d endpoints", count)
		if resolvedOut != "" {
			fmt.Fprintf(os.Stderr, " → %s", resolvedOut)
		}
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr)
	}
}

func printExportSummary(label, out string) {
	if out != "" {
		fmt.Fprintf(os.Stderr, "\nExported %s → %s\n\n", label, out)
	}
}

// writeToFileOrStdout creates the file at path (if non-empty) and calls fn with it,
// or calls fn with stdout if path is empty.
func writeToFileOrStdout(path string, fn func(*os.File) error) error {
	if path == "" {
		return fn(os.Stdout)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck
	return fn(f)
}

// writeBytes writes data to path, or to stdout if path is empty.
func writeBytes(data []byte, path string) error {
	if path == "" {
		_, err := os.Stdout.Write(data)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
