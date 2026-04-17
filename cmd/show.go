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

// RunShow runs `probe show <METHOD> <PATH> [flags]`.
func RunShow(args []string, cfg *config.Config) {
	fs := flag.NewFlagSet("show", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: probe show <METHOD> <PATH> [flags]")
		fmt.Fprintln(os.Stderr, `       probe show "QUERY OperationName" [flags]`)
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "flags:")
		fs.PrintDefaults()
	}

	showCalls := fs.Bool("calls", false, "show individual observations")
	jsonOut := fs.Bool("json", false, "output as JSON")
	db := fs.String("db", "", "override DB path")

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	rest := fs.Args()

	var method, path string
	switch len(rest) {
	case 0:
		fmt.Fprintln(os.Stderr, "probe show: METHOD and PATH are required")
		fs.Usage()
		os.Exit(1)
	case 1:
		// Single quoted arg: "QUERY ListUsers" or "GET /users"
		parts := strings.SplitN(rest[0], " ", 2)
		if len(parts) != 2 {
			fmt.Fprintf(os.Stderr, "probe show: cannot parse %q — expected \"METHOD PATH\"\n", rest[0])
			os.Exit(1)
		}
		method = strings.ToUpper(parts[0])
		path = parts[1]
	default:
		method = strings.ToUpper(rest[0])
		path = rest[1]
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

	var found *store.Endpoint
	for i := range endpoints {
		if strings.EqualFold(endpoints[i].Method, method) && endpoints[i].PathPattern == path {
			found = &endpoints[i]
			break
		}
	}

	if found == nil {
		fmt.Fprintf(os.Stderr, "probe: endpoint not found: %s %s\n", method, path)
		os.Exit(1)
	}

	fieldConf, err := s.GetFieldConfidence(found.ID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "probe: get field confidence: %v\n", err)
		os.Exit(1)
	}

	variants, err := s.GetVariants(found.ID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "probe: get variants: %v\n", err)
		os.Exit(1)
	}

	var observations []store.Observation
	if *showCalls {
		observations, err = s.GetObservations(found.ID, 20)
		if err != nil {
			fmt.Fprintf(os.Stderr, "probe: get observations: %v\n", err)
			os.Exit(1)
		}
	}

	if *jsonOut {
		type jsonOut struct {
			Endpoint     store.Endpoint              `json:"endpoint"`
			FieldConf    []store.FieldConfidenceRow  `json:"field_confidence"`
			Variants     []store.RequestVariant      `json:"variants,omitempty"`
			Observations []store.Observation         `json:"observations,omitempty"`
		}
		if err := render.PrintJSON(os.Stdout, jsonOut{
			Endpoint:     *found,
			FieldConf:    fieldConf,
			Variants:     variants,
			Observations: observations,
		}, cfg.Output.JSONIndent); err != nil {
			fmt.Fprintf(os.Stderr, "probe: render json: %v\n", err)
			os.Exit(1)
		}
		return
	}

	render.PrintDetail(os.Stdout, *found, fieldConf, observations, render.DetailOptions{
		NoColor:   cfg.Output.NoColor,
		ShowCalls: *showCalls,
		Variants:  variants,
	})
}
