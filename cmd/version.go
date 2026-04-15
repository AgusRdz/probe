package cmd

import "fmt"

// Version is set by main before the switch dispatch.
var Version = "dev"

// RunVersion prints the probe version string.
func RunVersion(_ []string) {
	fmt.Printf("probe %s\n", Version)
}
