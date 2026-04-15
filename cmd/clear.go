package cmd

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/AgusRdz/probe/config"
	"github.com/AgusRdz/probe/store"
)

// RunClear runs `probe clear [flags]`.
func RunClear(args []string, cfg *config.Config) {
	fs := flag.NewFlagSet("clear", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: probe clear [flags]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "flags:")
		fs.PrintDefaults()
	}

	endpoint := fs.String("endpoint", "", `delete a single endpoint, e.g. "GET /users"`)
	yes := fs.Bool("yes", false, "skip confirmation prompt")
	db := fs.String("db", "", "override DB path")

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	// cfg accepted for future use.
	_ = cfg

	s, err := store.Open(*db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "probe: open store: %v\n", err)
		os.Exit(1)
	}
	defer s.Close() //nolint:errcheck

	if *endpoint != "" {
		// Delete a single endpoint.
		parts := strings.SplitN(*endpoint, " ", 2)
		if len(parts) != 2 {
			fmt.Fprintf(os.Stderr, "probe clear: cannot parse --endpoint %q — expected \"METHOD PATH\"\n", *endpoint)
			os.Exit(1)
		}
		method := strings.ToUpper(parts[0])
		path := parts[1]

		if !*yes && !confirmPrompt(fmt.Sprintf("Delete endpoint %s %s?", method, path)) {
			fmt.Fprintln(os.Stderr, "probe clear: aborted")
			return
		}

		endpoints, err := s.GetEndpoints()
		if err != nil {
			fmt.Fprintf(os.Stderr, "probe: get endpoints: %v\n", err)
			os.Exit(1)
		}

		var found *store.Endpoint
		for i := range endpoints {
			if strings.EqualFold(endpoints[i].Method, method) && endpoints[i].PathPattern == path {
				found = &endpoints[i]
				break
			}
		}

		if found == nil {
			fmt.Fprintf(os.Stderr, "probe clear: endpoint not found: %s %s\n", method, path)
			os.Exit(1)
		}

		if err := s.DeleteEndpoint(found.ID); err != nil {
			fmt.Fprintf(os.Stderr, "probe: delete endpoint: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("probe: deleted %s %s\n", method, path)
		return
	}

	// Delete all.
	if !*yes && !confirmPrompt("Delete all observations? [y/N]") {
		fmt.Fprintln(os.Stderr, "probe clear: aborted")
		return
	}

	if err := s.DeleteAll(); err != nil {
		fmt.Fprintf(os.Stderr, "probe: delete all: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("probe: all observations deleted")
}

// confirmPrompt prints prompt and reads a line from stdin.
// Returns true if the user types "y" or "Y".
func confirmPrompt(prompt string) bool {
	fmt.Fprintf(os.Stderr, "%s [y/N] ", prompt)
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		return strings.EqualFold(line, "y")
	}
	return false
}
