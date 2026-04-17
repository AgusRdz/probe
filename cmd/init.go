package cmd

import (
	"flag"
	"fmt"
	"os"

	"github.com/AgusRdz/probe/config"
)

// RunInit runs `probe init [--global]`.
// Creates a config file from the current template.
// If the file already exists, writes a <path>.new alongside it for diffing.
func RunInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: probe init [--global]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Creates a config file with all available settings as commented examples.")
		fmt.Fprintln(os.Stderr, "If the file already exists, writes <file>.new for you to diff against.")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "flags:")
		fs.PrintDefaults()
	}

	global := fs.Bool("global", false, "init global config (~/.config/probe/config.yml) instead of project (.probe.yml)")

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	var path string
	if *global {
		path = config.Path()
	} else {
		path = config.ProjectPath()
	}

	_, err := os.Stat(path)
	exists := !os.IsNotExist(err)

	if !exists {
		if err := writeConfigTemplate(path); err != nil {
			fmt.Fprintf(os.Stderr, "probe init: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Created %s\n", path)
		fmt.Println("All settings are commented out — uncomment and edit what you need.")
		return
	}

	// File exists — write a .new file so the user can diff.
	newPath := path + ".new"
	if err := writeConfigTemplate(newPath); err != nil {
		fmt.Fprintf(os.Stderr, "probe init: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Config already exists: %s\n", path)
	fmt.Printf("Wrote current template: %s\n\n", newPath)
	fmt.Println("Diff to see what's new:")
	fmt.Printf("  diff %s %s\n\n", path, newPath)
	fmt.Println("Copy any new settings you want, then delete the .new file.")
}
