package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/AgusRdz/probe/observer"
	"github.com/AgusRdz/probe/store"
)

// DetailOptions controls detail rendering.
type DetailOptions struct {
	NoColor   bool
	ShowCalls bool // --calls flag: show individual observations
}

// PrintDetail writes full endpoint detail to w:
//   - Method + path pattern + source + framework
//   - Source file + line (if from scan)
//   - Description + tags (if set)
//   - Request schema with field confidence (table: field | type | format | confidence | required)
//   - Response schema (same format, per status code)
//   - If ShowCalls: list recent observations with status code + latency
func PrintDetail(
	w io.Writer,
	ep store.Endpoint,
	fieldConf []store.FieldConfidenceRow,
	observations []store.Observation,
	opts DetailOptions,
) {
	nl := func() { fmt.Fprintln(w) }
	heading := func(s string) {
		fmt.Fprintln(w, colorize(colorize(s, colorBoldStr, opts.NoColor), colorCyanStr, opts.NoColor))
	}
	label := func(k, v string) {
		fmt.Fprintf(w, "  %s  %s\n",
			colorize(fmt.Sprintf("%-14s", k+":"), colorDimStr, opts.NoColor),
			v,
		)
	}

	// ── Header ────────────────────────────────────────────────────────────────
	methodColored := colorize(ep.Method, colorBoldStr, opts.NoColor)
	pathColored := colorize(ep.PathPattern, colorBoldStr, opts.NoColor)
	heading(fmt.Sprintf("%s %s", methodColored, pathColored))
	nl()

	label("Source", colorizeSource(ep.Source, opts.NoColor))
	label("Protocol", ep.Protocol)

	if ep.Framework != "" {
		label("Framework", ep.Framework)
	}
	if ep.SourceFile != "" {
		loc := ep.SourceFile
		if ep.SourceLine > 0 {
			loc = fmt.Sprintf("%s:%d", ep.SourceFile, ep.SourceLine)
		}
		label("Source file", loc)
	}

	label("Calls", fmt.Sprintf("%d", ep.CallCount))

	conf := endpointConfidence(ep)
	label("Confidence", confidenceBarColored(conf, opts.NoColor))

	if ep.Deprecated {
		label("Deprecated", colorize("yes", colorYellowStr, opts.NoColor))
	}
	if ep.RequiresAuth {
		label("Auth", colorize("required", colorYellowStr, opts.NoColor))
	}
	if ep.Description != "" {
		label("Description", ep.Description)
	}
	if len(ep.Tags) > 0 {
		label("Tags", strings.Join(ep.Tags, ", "))
	}

	nl()

	// ── Request schema ────────────────────────────────────────────────────────
	reqFields := filterFieldConf(fieldConf, "request")
	if len(reqFields) > 0 {
		heading("Request fields")
		nl()
		printFieldTable(w, reqFields, opts.NoColor)
		nl()
	}

	// ── Response schema (grouped by status code) ──────────────────────────────
	respFields := filterFieldConf(fieldConf, "response")
	if len(respFields) > 0 {
		heading("Response fields")
		nl()
		printFieldTable(w, respFields, opts.NoColor)
		nl()
	}

	// ── Observations ──────────────────────────────────────────────────────────
	if opts.ShowCalls && len(observations) > 0 {
		heading(fmt.Sprintf("Recent observations (%d)", len(observations)))
		nl()
		printObservations(w, observations, opts.NoColor)
		nl()
	}
}

// filterFieldConf returns only rows matching the given location.
func filterFieldConf(rows []store.FieldConfidenceRow, location string) []store.FieldConfidenceRow {
	out := make([]store.FieldConfidenceRow, 0, len(rows))
	for _, r := range rows {
		if r.Location == location {
			out = append(out, r)
		}
	}
	return out
}

// printFieldTable renders field confidence as an aligned table.
// Columns: FIELD | TYPE | FORMAT | CONFIDENCE | REQUIRED
func printFieldTable(w io.Writer, rows []store.FieldConfidenceRow, noColor bool) {
	type fieldRow struct {
		field    string
		typ      string
		format   string
		confBar  string // plain for width
		required string
		confVal  float64
	}

	const (
		hField    = "FIELD"
		hType     = "TYPE"
		hFormat   = "FORMAT"
		hConf     = "CONFIDENCE"
		hRequired = "REQUIRED"
	)

	wField, wType, wFormat, wConf :=
		len(hField), len(hType), len(hFormat), len(hConf)
	_ = len(hRequired) // column width fixed — always "REQUIRED"

	frows := make([]fieldRow, 0, len(rows))
	for _, r := range rows {
		var schema observer.Schema
		if r.TypeJSON != "" {
			_ = json.Unmarshal([]byte(r.TypeJSON), &schema)
		}

		var conf float64
		if r.TotalCalls > 0 {
			conf = float64(r.SeenCount) / float64(r.TotalCalls)
		}

		bar := confidenceBar(conf)
		pct := confidencePct(conf)
		confPlain := bar + " " + pct

		required := "no"
		if conf >= 0.9 {
			required = "yes"
		}

		fr := fieldRow{
			field:    r.FieldPath,
			typ:      schema.Type,
			format:   schema.Format,
			confBar:  confPlain,
			required: required,
			confVal:  conf,
		}
		frows = append(frows, fr)

		if len(fr.field) > wField {
			wField = len(fr.field)
		}
		if len(fr.typ) > wType {
			wType = len(fr.typ)
		}
		if len(fr.format) > wFormat {
			wFormat = len(fr.format)
		}
		if len(confPlain) > wConf {
			wConf = len(confPlain)
		}
	}

	// Header.
	header := fmt.Sprintf("  %-*s  %-*s  %-*s  %-*s  %s",
		wField, hField, wType, hType, wFormat, hFormat, wConf, hConf, hRequired)
	fmt.Fprintln(w, colorize(header, colorDimStr, noColor))

	sep := "  " + strings.Repeat("─", len(header)-2)
	fmt.Fprintln(w, colorize(sep, colorDimStr, noColor))

	for _, fr := range frows {
		confColored := confidenceBarColored(fr.confVal, noColor)
		confPadded := confColored + strings.Repeat(" ", wConf-len(fr.confBar))

		reqColored := fr.required
		if !noColor {
			if fr.required == "yes" {
				reqColored = colorize(fr.required, colorGreenStr, false)
			} else {
				reqColored = colorize(fr.required, colorDimStr, false)
			}
		}

		fmt.Fprintf(w, "  %-*s  %-*s  %-*s  %s  %s\n",
			wField, fr.field,
			wType, fr.typ,
			wFormat, fr.format,
			confPadded,
			reqColored,
		)
	}
}

// printObservations renders a compact table of recent observations.
func printObservations(w io.Writer, obs []store.Observation, noColor bool) {
	const (
		hTime    = "TIME"
		hStatus  = "STATUS"
		hLatency = "LATENCY"
	)

	wTime, wStatus, wLatency := len(hTime), len(hStatus), len(hLatency)

	type obsRow struct {
		time    string
		status  string
		latency string
		code    int
	}

	orows := make([]obsRow, 0, len(obs))
	for _, o := range obs {
		t := o.ObservedAt.Format("2006-01-02 15:04:05")
		status := fmt.Sprintf("%d", o.StatusCode)
		latency := fmt.Sprintf("%dms", o.LatencyMs)

		or := obsRow{
			time:    t,
			status:  status,
			latency: latency,
			code:    o.StatusCode,
		}
		orows = append(orows, or)

		if len(t) > wTime {
			wTime = len(t)
		}
		if len(status) > wStatus {
			wStatus = len(status)
		}
		if len(latency) > wLatency {
			wLatency = len(latency)
		}
	}

	header := fmt.Sprintf("  %-*s  %-*s  %s", wTime, hTime, wStatus, hStatus, hLatency)
	fmt.Fprintln(w, colorize(header, colorDimStr, noColor))
	sep := "  " + strings.Repeat("─", len(header)-2)
	fmt.Fprintln(w, colorize(sep, colorDimStr, noColor))

	for _, or := range orows {
		statusColored := or.status
		if !noColor {
			switch {
			case or.code >= 200 && or.code < 300:
				statusColored = colorize(or.status, colorGreenStr, false)
			case or.code >= 400 && or.code < 500:
				statusColored = colorize(or.status, colorYellowStr, false)
			case or.code >= 500:
				statusColored = colorize(or.status, colorRedStr, false)
			}
		}
		statusPadded := statusColored + strings.Repeat(" ", wStatus-len(or.status))

		fmt.Fprintf(w, "  %-*s  %s  %s\n",
			wTime, or.time,
			statusPadded,
			or.latency,
		)
	}
}
