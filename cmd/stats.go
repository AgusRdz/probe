package cmd

import (
	"flag"
	"fmt"
	"os"

	"github.com/AgusRdz/probe/config"
	"github.com/AgusRdz/probe/store"
)

// RunStats runs `probe stats [flags]`.
func RunStats(args []string, cfg *config.Config) {
	fs := flag.NewFlagSet("stats", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: probe stats [flags]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "flags:")
		fs.PrintDefaults()
	}

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

	stats, err := s.Stats()
	if err != nil {
		fmt.Fprintf(os.Stderr, "probe: stats: %v\n", err)
		os.Exit(1)
	}

	// cfg is accepted for future use (e.g. --no-color) but not yet needed here.
	_ = cfg

	fmt.Printf("  Total endpoints:  %d\n", stats["total"])
	fmt.Printf("  Observed:         %d\n", stats["observed"])
	fmt.Printf("  Scan only:        %d\n", stats["scan"])
	fmt.Printf("  Scan + observed:  %d\n", stats["scan+obs"])
}
