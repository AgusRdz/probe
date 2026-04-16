package cmd

import (
	"fmt"
	"os"

	"github.com/AgusRdz/probe/updater"
)

// RunUpdate executes the probe update subcommand.
func RunUpdate(args []string) {
	if err := updater.RunUpdate(Version); err != nil {
		fmt.Fprintf(os.Stderr, "probe update: %v\n", err)
		os.Exit(1)
	}
}
