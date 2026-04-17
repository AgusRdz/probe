package cmd

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/AgusRdz/probe/config"
	"github.com/AgusRdz/probe/render"
	"github.com/AgusRdz/probe/store"
)

// RunList runs `probe list [flags]`.
func RunList(args []string, cfg *config.Config) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: probe list [flags]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "flags:")
		fs.PrintDefaults()
	}

	jsonOut := fs.Bool("json", false, "output as JSON array")
	minCalls := fs.Int("min-calls", 0, "only show endpoints with at least N calls")
	source := fs.String("source", "", `filter by source: "scan", "observed", "scan+obs"`)
	protocol := fs.String("protocol", "", `filter by protocol: "rest", "graphql", "grpc"`)
	db := fs.String("db", "", "override DB path")
	cols := fs.String("cols", "", `columns to display, comma-separated (default from config or: method,path,source,file,calls,coverage)
    method     HTTP verb (GET, POST, …)
    path       URL pattern with {param} placeholders
    source     where probe learned about the endpoint: scan / observed / scan+obs
    file       source file and line number (scan only)
    calls      number of observed traffic calls
    coverage   schema evidence strength — green bar shows how well this endpoint is documented
               (based on call count × source quality; scan-only starts at 35%, grows with traffic)
    protocol   rest / graphql / grpc
    status     observed HTTP status codes
    framework  detected framework (e.g. aspnet-mvc, nestjs)
    auth       whether the endpoint requires authentication (yes / -)
    example:   --cols method,path,source,file,calls`)

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	columns := render.DefaultColumns
	if *cols != "" {
		columns = splitCols(*cols)
	} else if cfg.List.Columns != "" {
		columns = splitCols(cfg.List.Columns)
	}

	s, err := store.Open(*db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "probe: open store: %v\n", err)
		os.Exit(1)
	}
	defer s.Close() //nolint:errcheck

	endpoints, err := s.GetEndpoints()
	if err != nil {
		fmt.Fprintf(os.Stderr, "probe: get endpoints: %v\n", err)
		os.Exit(1)
	}

	opts := render.TableOptions{
		NoColor:  cfg.Output.NoColor,
		JSON:     *jsonOut,
		MinCalls: *minCalls,
		Source:   *source,
		Protocol: *protocol,
		Columns:  columns,
	}

	render.PrintTable(os.Stdout, endpoints, nil, opts)
}

func splitCols(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
