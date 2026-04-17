package cmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/AgusRdz/probe/config"
	"github.com/AgusRdz/probe/scanner"
	"github.com/AgusRdz/probe/store"
)

// RunScan runs `probe scan [flags]`.
func RunScan(args []string, cfg *config.Config) {
	fs := flag.NewFlagSet("scan", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: probe scan [flags]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "flags:")
		fs.PrintDefaults()
	}

	dir := fs.String("dir", ".", "directory to scan")
	framework := fs.String("framework", "", "comma-separated frameworks to force (skips auto-detect)")
	dryRun := fs.Bool("dry-run", false, "print found endpoints without storing to DB")
	verbose := fs.Bool("verbose", false, "show which files matched which patterns")
	db := fs.String("db", "", "override DB path")
	jsonOut := fs.Bool("json", false, "output as JSON instead of table")

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	// Build ScanConfig: flags override cfg.Scan.
	scanCfg := &config.ScanConfig{
		Dir:        cfg.Scan.Dir,
		Frameworks: cfg.Scan.Frameworks,
		Exclude:    cfg.Scan.Exclude,
	}
	if *dir != "." {
		scanCfg.Dir = *dir
	}
	if *framework != "" {
		parts := strings.Split(*framework, ",")
		trimmed := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				trimmed = append(trimmed, p)
			}
		}
		scanCfg.Frameworks = trimmed
	}

	// Effective scan directory: flag wins over config default.
	scanDir := *dir
	if scanDir == "." && scanCfg.Dir != "" && scanCfg.Dir != "./" {
		scanDir = scanCfg.Dir
	}

	fmt.Fprintf(os.Stdout, "\nScanning %s...\n\n", scanDir)

	endpoints, err := scanner.Run(scanDir, scanCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "probe scan: %v\n", err)
		os.Exit(1)
	}

	// Collect detected frameworks from results.
	detectedSet := make(map[string]struct{})
	for _, ep := range endpoints {
		if ep.Framework != "" {
			detectedSet[ep.Framework] = struct{}{}
		}
	}
	if len(detectedSet) > 0 {
		names := make([]string, 0, len(detectedSet))
		for k := range detectedSet {
			names = append(names, k)
		}
		fmt.Fprintf(os.Stdout, "  Detected: %s\n\n", strings.Join(names, ", "))
	}

	if *dryRun {
		printScanResults(os.Stdout, endpoints, *jsonOut, *verbose)
		fmt.Fprintf(os.Stdout, "\n  %d endpoints found (dry run — nothing stored).\n\n", len(endpoints))
		return
	}

	// Persist to store — derive DB name from scan directory when not overridden.
	dbPath := *db
	if dbPath == "" {
		var err2 error
		dbPath, err2 = store.DBPathForDir(scanDir)
		if err2 != nil {
			fmt.Fprintf(os.Stderr, "probe scan: resolve db path: %v\n", err2)
			os.Exit(1)
		}
	}
	s, err := store.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "probe scan: open store: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, "  DB: %s\n\n", dbPath)
	defer s.Close() //nolint:errcheck

	var newCount, updatedCount int
	for _, ep := range endpoints {
		wasNew, err := s.UpsertScannedEndpoint(store.ScannedEndpointInput{
			Method:       ep.Method,
			PathPattern:  ep.PathPattern,
			Protocol:     ep.Protocol,
			Framework:    ep.Framework,
			SourceFile:   ep.SourceFile,
			SourceLine:   ep.SourceLine,
			ReqSchema:    ep.ReqSchema,
			RespSchema:   ep.RespSchema,
			StatusCodes:  ep.StatusCodes,
			Description:  ep.Description,
			Tags:         ep.Tags,
			Deprecated:   ep.Deprecated,
			RequiresAuth: ep.RequiresAuth,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "probe scan: upsert %s %s: %v\n", ep.Method, ep.PathPattern, err)
			continue
		}
		if wasNew {
			newCount++
		} else {
			updatedCount++
		}
	}

	printScanResults(os.Stdout, endpoints, *jsonOut, *verbose)
	fmt.Fprintf(os.Stdout, "\n  %d endpoints stored (%d new, %d updated).\n\n",
		len(endpoints), newCount, updatedCount)
}

// printScanResults writes the scan endpoint list to w.
func printScanResults(w *os.File, endpoints []scanner.ScannedEndpoint, asJSON bool, verbose bool) {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(endpoints)
		return
	}

	// Compute column widths.
	methodW, pathW, sourceW, frameworkW := 6, 4, 6, 9
	for _, ep := range endpoints {
		if len(ep.Method) > methodW {
			methodW = len(ep.Method)
		}
		if len(ep.PathPattern) > pathW {
			pathW = len(ep.PathPattern)
		}
		if len(ep.Framework) > frameworkW {
			frameworkW = len(ep.Framework)
		}
	}
	_ = sourceW // always "scan"

	for _, ep := range endpoints {
		loc := ""
		if verbose && ep.SourceFile != "" {
			loc = fmt.Sprintf("%s:%d", ep.SourceFile, ep.SourceLine)
		} else if ep.SourceFile != "" {
			loc = fmt.Sprintf("%s:%d", ep.SourceFile, ep.SourceLine)
		}
		fmt.Fprintf(w, "  %-*s  %-*s  scan   %-*s  %s\n",
			methodW, ep.Method,
			pathW, ep.PathPattern,
			frameworkW, ep.Framework,
			loc,
		)
	}
}
