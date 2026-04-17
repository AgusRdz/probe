package cmd

import (
	"bufio"
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
	format        := fs.String("format", cfg.Export.DefaultFormat, "output format: openapi (YAML), json (OpenAPI JSON), swagger (2.0), postman, curl, httpie, bruno")
	out           := fs.String("out", "", "output file or directory path (overrides config and smart default)")
	minCalls      := fs.Int("min-calls", cfg.Export.MinCalls, "only export endpoints with at least N traffic calls (0 = include scan-only too)")
	db            := fs.String("db", "", "override DB path")
	title         := fs.String("title", "", "collection/spec title (overrides config info_title)")
	noInteractive := fs.Bool("no-interactive", false, "disable interactive conflict resolution (conflicts default to keep)")

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

	if *title != "" {
		cfg.Export.InfoTitle = *title
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

	// Resolve output path — priority order:
	// 1. --out flag (always wins)
	// 2. cfg.Export.Outputs[format] (per-format override in config)
	// 3. cfg.Export.OutputDir/<base><ext> (shared output directory in config)
	// 4. <cwd-basename><ext> when using a shorthand flag
	// 5. stdout (--format without --out and no config)
	resolvedOut := *out
	if resolvedOut == "" {
		if cfgOut, ok := cfg.Export.Outputs[resolvedFormat]; ok && cfgOut != "" {
			resolvedOut = cfgOut
		} else if cfg.Export.OutputDir != "" {
			cwd, _ := os.Getwd()
			base := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(filepath.Base(cwd)), "-"), "-")
			if base == "" {
				base = "api"
			}
			resolvedOut = filepath.Join(cfg.Export.OutputDir, base+formatExtensions[resolvedFormat])
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
		if resolvedOut != "" {
			if existing, statErr := os.ReadFile(resolvedOut); statErr == nil {
				merged, added := export.MergeScript(existing, data)
				printScriptMergeSummary(resolvedOut, added)
				if err := writeBytes(merged, resolvedOut); err != nil {
					fmt.Fprintf(os.Stderr, "probe: write merged curl: %v\n", err)
					os.Exit(1)
				}
				break
			}
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
		if resolvedOut != "" {
			if existing, statErr := os.ReadFile(resolvedOut); statErr == nil {
				merged, added := export.MergeScript(existing, data)
				printScriptMergeSummary(resolvedOut, added)
				if err := writeBytes(merged, resolvedOut); err != nil {
					fmt.Fprintf(os.Stderr, "probe: write merged httpie: %v\n", err)
					os.Exit(1)
				}
				break
			}
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
		if resolvedOut != "" {
			if _, statErr := os.Stat(resolvedOut); statErr == nil {
				runSpecMerge(swaggerPathsToOpenAPI(spec.Paths), resolvedOut, "swagger")
				break
			}
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
		// Auto-merge: if the directory already exists with .bru files, merge.
		added, conflicts, err := export.ComputeBrunoMerge(dir, collection)
		if err != nil {
			fmt.Fprintf(os.Stderr, "probe: compute bruno merge: %v\n", err)
			os.Exit(1)
		}
		if len(added) > 0 || len(conflicts) > 0 {
			fmt.Fprintf(os.Stderr, "\nprobe: merging Bruno collection: %s/ (%d new, %d conflict(s))\n\n",
				dir, len(added), len(conflicts))
			for _, name := range added {
				fmt.Fprintf(os.Stderr, "  added    %s\n", name)
			}
			toWrite := added
			if len(conflicts) > 0 && !*noInteractive {
				scanner := bufio.NewScanner(os.Stdin)
				skipAll := false
				for _, c := range conflicts {
					if skipAll {
						fmt.Fprintf(os.Stderr, "  skipped  %s (conflict — kept existing)\n", c.Filename)
						continue
					}
					fmt.Fprintf(os.Stderr, "conflict: %s\n", c.Filename)
					fmt.Fprintf(os.Stderr, "  [k]eep existing  [r]eplace  [s]kip all conflicts: ")
					resolution := "keep"
					if scanner.Scan() {
						switch strings.TrimSpace(strings.ToLower(scanner.Text())) {
						case "r":
							resolution = "replace"
						case "s":
							resolution = "keep"
							skipAll = true
						}
					}
					if resolution == "replace" {
						toWrite = append(toWrite, c.Filename)
					} else {
						fmt.Fprintf(os.Stderr, "  skipped  %s (conflict — kept existing)\n", c.Filename)
					}
					fmt.Fprintln(os.Stderr)
				}
			} else {
				for _, c := range conflicts {
					fmt.Fprintf(os.Stderr, "  skipped  %s (conflict — kept existing)\n", c.Filename)
				}
			}
			if err := export.ApplyBrunoMerge(dir, collection, toWrite); err != nil {
				fmt.Fprintf(os.Stderr, "probe: apply bruno merge: %v\n", err)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "\nprobe: wrote %d file(s) → %s/\n\n", len(toWrite), dir)
			break
		}
		// Fresh write — no existing .bru files or all identical.
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

		// Auto-merge: if the output file already exists, merge instead of overwrite.
		if resolvedOut != "" {
			if _, statErr := os.Stat(resolvedOut); statErr == nil {
				runPostmanMerge(col, resolvedOut, *noInteractive)
				break
			}
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
		if resolvedOut != "" {
			if _, statErr := os.Stat(resolvedOut); statErr == nil {
				if resolvedFormat == "json" {
					runSpecMerge(spec.Paths, resolvedOut, "json")
				} else {
					runSpecMerge(spec.Paths, resolvedOut, "openapi")
				}
				break
			}
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

// runPostmanMerge loads the existing collection at path, diffs it against the
// freshly generated col, resolves conflicts interactively (or defaults to keep
// when noInteractive is true), and writes the merged result back to path.
func runPostmanMerge(col *export.PostmanCollection, path string, noInteractive bool) {
	existing, err := export.LoadExistingCollection(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "probe: read existing collection: %v\n", err)
		os.Exit(1)
	}

	result, err := export.ComputeMerge(existing, col)
	if err != nil {
		fmt.Fprintf(os.Stderr, "probe: compute merge: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "\nprobe: reading %s (%d items)\n", path, len(existing.Items))
	fmt.Fprintf(os.Stderr, "probe: %d new endpoint(s), %d conflict(s)\n\n",
		len(result.Added), len(result.Conflicts))

	for _, a := range result.Added {
		fmt.Fprintf(os.Stderr, "  added    %s\n", a.Name)
	}
	for _, u := range result.Unchanged {
		fmt.Fprintf(os.Stderr, "  skipped  %s (already exists, no changes)\n", u)
	}

	resolutions := make(map[string]string, len(result.Conflicts))

	if len(result.Conflicts) > 0 && !noInteractive {
		fmt.Fprintln(os.Stderr)
		scanner := bufio.NewScanner(os.Stdin)
		skipAll := false
		for _, c := range result.Conflicts {
			if skipAll {
				resolutions[c.Key] = "keep"
				continue
			}
			existBody := ""
			if c.Existing.Request.Body != nil {
				existBody = c.Existing.Request.Body.Raw
			}
			incomingBody := ""
			if c.Incoming.Request.Body != nil {
				incomingBody = c.Incoming.Request.Body.Raw
			}
			fmt.Fprintf(os.Stderr, "conflict: %s\n", c.Key)
			fmt.Fprintf(os.Stderr, "  current body:  %s\n", existBody)
			fmt.Fprintf(os.Stderr, "  probe body:    %s\n\n", incomingBody)
			fmt.Fprintf(os.Stderr, "  [k]eep current  [r]eplace  [m]erge (add missing fields)  [s]kip all conflicts: ")

			resolution := "keep"
			if scanner.Scan() {
				switch strings.TrimSpace(strings.ToLower(scanner.Text())) {
				case "r":
					resolution = "replace"
				case "m":
					resolution = "merge"
				case "s":
					resolution = "keep"
					skipAll = true
				}
			}
			resolutions[c.Key] = resolution
			fmt.Fprintln(os.Stderr)
		}
	}

	merged, err := export.BuildMergedCollection(existing, col, result, resolutions)
	if err != nil {
		fmt.Fprintf(os.Stderr, "probe: build merged collection: %v\n", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "probe: create directories: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(path, merged, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "probe: write merged collection: %v\n", err)
		os.Exit(1)
	}

	totalItems := len(existing.Items) + len(result.Added)
	fmt.Fprintf(os.Stderr, "\nprobe: wrote %d items → %s\n\n", totalItems, path)
}

// runSpecMerge loads an existing YAML or JSON spec file, merges new
// path+method entries from incoming, and writes the result back to path.
// format is "openapi" (YAML), "json" (JSON), or "swagger" (YAML).
// Existing entries are never modified — add-only merge.
func runSpecMerge(incoming map[string]export.OpenAPIPathItem, path, format string) {
	incomingSpec := &export.OpenAPISpec{Paths: incoming}

	var existingMap map[string]interface{}
	var loadErr error
	if format == "json" {
		existingMap, loadErr = export.LoadExistingJSONMap(path)
	} else {
		existingMap, loadErr = export.LoadExistingYAMLMap(path)
	}
	if loadErr != nil {
		fmt.Fprintf(os.Stderr, "probe: read existing spec: %v\n", loadErr)
		os.Exit(1)
	}

	added := export.MergeOpenAPIPaths(existingMap, incomingSpec)

	fmt.Fprintf(os.Stderr, "\nprobe: merging %s (%d new endpoint(s)) → %s\n\n",
		format, len(added), path)
	for _, key := range added {
		fmt.Fprintf(os.Stderr, "  added    %s\n", key)
	}
	if len(added) == 0 {
		fmt.Fprintf(os.Stderr, "  (no new endpoints)\n")
	}
	fmt.Fprintln(os.Stderr)

	var out []byte
	var serErr error
	if format == "json" {
		out, serErr = export.SerializeMergedJSON(existingMap)
	} else {
		out, serErr = export.SerializeMergedYAML(existingMap)
	}
	if serErr != nil {
		fmt.Fprintf(os.Stderr, "probe: serialize merged spec: %v\n", serErr)
		os.Exit(1)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "probe: create directories: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "probe: write merged spec: %v\n", err)
		os.Exit(1)
	}
}

// swaggerPathsToOpenAPI converts Swagger 2.0 path items to OpenAPI path items
// for the purpose of path merge (method names are identical).
func swaggerPathsToOpenAPI(swaggerPaths map[string]export.SwaggerPathItem) map[string]export.OpenAPIPathItem {
	result := make(map[string]export.OpenAPIPathItem, len(swaggerPaths))
	for path, item := range swaggerPaths {
		var oa export.OpenAPIPathItem
		if item.Get != nil {
			oa.Get = &export.OpenAPIOperation{}
		}
		if item.Post != nil {
			oa.Post = &export.OpenAPIOperation{}
		}
		if item.Put != nil {
			oa.Put = &export.OpenAPIOperation{}
		}
		if item.Patch != nil {
			oa.Patch = &export.OpenAPIOperation{}
		}
		if item.Delete != nil {
			oa.Delete = &export.OpenAPIOperation{}
		}
		if item.Head != nil {
			oa.Head = &export.OpenAPIOperation{}
		}
		if item.Options != nil {
			oa.Options = &export.OpenAPIOperation{}
		}
		result[path] = oa
	}
	return result
}

// printScriptMergeSummary prints a summary of a script merge operation.
func printScriptMergeSummary(path string, added []string) {
	fmt.Fprintf(os.Stderr, "\nprobe: merging script (%d new endpoint(s)) → %s\n\n", len(added), path)
	for _, key := range added {
		fmt.Fprintf(os.Stderr, "  added    %s\n", key)
	}
	if len(added) == 0 {
		fmt.Fprintf(os.Stderr, "  (no new endpoints)\n")
	}
	fmt.Fprintln(os.Stderr)
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
