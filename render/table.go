package render

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/AgusRdz/probe/store"
)

// TableOptions controls table rendering.
type TableOptions struct {
	NoColor  bool
	JSON     bool   // if true, render as JSON array instead of table
	MinCalls int    // 0 = show all
	Source   string // "" = all, "scan", "observed", "scan+obs"
	Protocol string // "" = all, "rest", "graphql", "grpc", etc.
}

// PrintTable writes the endpoint list to w.
// Output format matches PLAN.md probe list output:
//
//	METHOD  PATH  SOURCE  CALLS  CONFIDENCE  PROTOCOL  STATUS CODES
//
// Columns are aligned. Confidence shown as bar + percentage.
// SOURCE column: "scan" (yellow), "observed" (green), "scan+obs" (cyan).
// Endpoints with 0 calls show "—" for status codes.
// Low confidence (< 30%) shown with "← low confidence" annotation.
// Unconfirmed path patterns (ending in "?") noted.
//
// statusCodes maps endpointID → sorted status codes. Pass nil for fast mode (shows "—").
func PrintTable(w io.Writer, endpoints []store.Endpoint, statusCodes map[int64][]int, opts TableOptions) {
	filtered := filterEndpoints(endpoints, opts)
	if len(filtered) == 0 {
		fmt.Fprintln(w, colorize("  No endpoints found.", colorDimStr, opts.NoColor))
		return
	}

	// Compute column widths.
	const (
		colMethod   = 0
		colPath     = 1
		colSource   = 2
		colCalls    = 3
		colConf     = 4
		colProtocol = 5
		colStatus   = 6
	)

	headers := []string{"METHOD", "PATH", "SOURCE", "CALLS", "CONFIDENCE", "PROTOCOL", "STATUS CODES"}
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}

	type row struct {
		method   string
		path     string
		source   string
		calls    string
		conf     string // bar + pct, plain (no ANSI) for width calc
		protocol string
		status   string
		note     string
		ep       store.Endpoint
		confVal  float64
	}

	rows := make([]row, 0, len(filtered))
	for _, ep := range filtered {
		conf := endpointConfidence(ep)
		bar := confidenceBar(conf)
		pct := confidencePct(conf)
		confPlain := bar + " " + pct // for width measurement (bar is ASCII blocks, no ANSI)

		sc := statusCodesStr(ep, statusCodes)

		var note string
		if conf < 0.30 && ep.CallCount > 0 {
			note = "← low confidence"
		} else if ep.CallCount == 0 {
			note = "← not yet seen"
		}

		r := row{
			method:   ep.Method,
			path:     ep.PathPattern,
			source:   ep.Source,
			calls:    fmt.Sprintf("%d", ep.CallCount),
			conf:     confPlain,
			protocol: ep.Protocol,
			status:   sc,
			note:     note,
			ep:       ep,
			confVal:  conf,
		}
		rows = append(rows, r)

		if len(r.method) > widths[colMethod] {
			widths[colMethod] = len(r.method)
		}
		if len(r.path) > widths[colPath] {
			widths[colPath] = len(r.path)
		}
		if len(r.source) > widths[colSource] {
			widths[colSource] = len(r.source)
		}
		if len(r.calls) > widths[colCalls] {
			widths[colCalls] = len(r.calls)
		}
		if len(confPlain) > widths[colConf] {
			widths[colConf] = len(confPlain)
		}
		if len(r.protocol) > widths[colProtocol] {
			widths[colProtocol] = len(r.protocol)
		}
		if len(r.status) > widths[colStatus] {
			widths[colStatus] = len(r.status)
		}
	}

	// Print header.
	headerLine := fmt.Sprintf("  %-*s  %-*s  %-*s  %-*s  %-*s  %-*s  %s",
		widths[colMethod], headers[colMethod],
		widths[colPath], headers[colPath],
		widths[colSource], headers[colSource],
		widths[colCalls], headers[colCalls],
		widths[colConf], headers[colConf],
		widths[colProtocol], headers[colProtocol],
		headers[colStatus],
	)
	fmt.Fprintln(w, colorize(headerLine, colorDimStr, opts.NoColor))

	// Print separator.
	sep := "  " + strings.Repeat("─", len(headerLine)-2)
	fmt.Fprintln(w, colorize(sep, colorDimStr, opts.NoColor))

	for _, r := range rows {
		sourceColored := colorizeSource(r.source, opts.NoColor)
		// Pad source accounting for ANSI escape codes added by colorizeSource.
		sourcePad := r.source // plain for padding calc
		sourcePadded := sourceColored + strings.Repeat(" ", widths[colSource]-len(sourcePad))

		confColored := confidenceBarColored(r.confVal, opts.NoColor)
		confPadded := confColored + strings.Repeat(" ", widths[colConf]-len(r.conf))

		line := fmt.Sprintf("  %-*s  %-*s  %s  %-*s  %s  %-*s  %s",
			widths[colMethod], r.method,
			widths[colPath], r.path,
			sourcePadded,
			widths[colCalls], r.calls,
			confPadded,
			widths[colProtocol], r.protocol,
			r.status,
		)

		if r.note != "" {
			line += "  " + colorize(r.note, colorDimStr, opts.NoColor)
		}

		fmt.Fprintln(w, line)
	}

	fmt.Fprintf(w, "\n  %s\n", colorize(fmt.Sprintf("%d endpoint(s)", len(rows)), colorDimStr, opts.NoColor))
}

// filterEndpoints applies MinCalls, Source, and Protocol filters.
func filterEndpoints(endpoints []store.Endpoint, opts TableOptions) []store.Endpoint {
	out := make([]store.Endpoint, 0, len(endpoints))
	for _, ep := range endpoints {
		if opts.MinCalls > 0 && ep.CallCount < opts.MinCalls {
			continue
		}
		if opts.Source != "" && ep.Source != opts.Source {
			continue
		}
		if opts.Protocol != "" && ep.Protocol != opts.Protocol {
			continue
		}
		out = append(out, ep)
	}
	return out
}

// endpointConfidence computes a display-time confidence proxy from call_count and source.
// Full per-field confidence requires a DB query — this gives a reasonable visual indicator.
//
//	source multipliers: "scan"=0.35, "scan+obs"=0.6, "observed"=1.0
//	formula: min(call_count/30, 1.0) * multiplier
func endpointConfidence(ep store.Endpoint) float64 {
	if ep.CallCount == 0 {
		return 0
	}
	var multiplier float64
	switch ep.Source {
	case "observed":
		multiplier = 1.0
	case "scan+obs":
		multiplier = 0.6
	default: // "scan"
		multiplier = 0.35
	}
	ratio := float64(ep.CallCount) / 30.0
	if ratio > 1.0 {
		ratio = 1.0
	}
	return ratio * multiplier
}

// confidenceBar returns an 8-char progress bar like "████░░░░".
func confidenceBar(conf float64) string {
	const total = 8
	filled := int(conf*total + 0.5)
	if filled > total {
		filled = total
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", total-filled)
}

// confidencePct returns "94%" for 0.94.
func confidencePct(conf float64) string {
	return fmt.Sprintf("%3.0f%%", conf*100)
}

// confidenceBarColored wraps the bar in a color based on confidence level.
// High (>=70%): green; mid (30-69%): yellow; low (<30%): red.
func confidenceBarColored(conf float64, noColor bool) string {
	bar := confidenceBar(conf)
	pct := confidencePct(conf)
	plain := bar + " " + pct

	if noColor {
		return plain
	}

	var c string
	switch {
	case conf >= 0.70:
		c = colorGreenStr
	case conf >= 0.30:
		c = colorYellowStr
	default:
		c = colorRedStr
	}
	return colorize(plain, c, false)
}

// statusCodesStr returns comma-joined status codes for the endpoint, or "—" if none.
func statusCodesStr(ep store.Endpoint, statusCodes map[int64][]int) string {
	if statusCodes == nil {
		if ep.CallCount == 0 {
			return "—"
		}
		return "—"
	}
	codes, ok := statusCodes[ep.ID]
	if !ok || len(codes) == 0 {
		return "—"
	}
	sort.Ints(codes)
	parts := make([]string, len(codes))
	for i, c := range codes {
		parts[i] = fmt.Sprintf("%d", c)
	}
	return strings.Join(parts, ", ")
}

// colorizeSource applies the source-specific color.
// "scan" → yellow, "observed" → green, "scan+obs" → cyan.
func colorizeSource(source string, noColor bool) string {
	if noColor {
		return source
	}
	switch source {
	case "scan":
		return colorize(source, colorYellowStr, false)
	case "observed":
		return colorize(source, colorGreenStr, false)
	case "scan+obs":
		return colorize(source, colorCyanStr, false)
	default:
		return source
	}
}

// ANSI color constants (render package — cannot import package main).
const (
	colorResetStr  = "\033[0m"
	colorBoldStr   = "\033[1m"
	colorDimStr    = "\033[2m"
	colorCyanStr   = "\033[36m"
	colorYellowStr = "\033[33m"
	colorRedStr    = "\033[31m"
	colorGreenStr  = "\033[32m"
)

// colorize wraps s with the given ANSI code and a reset, unless noColor is true.
func colorize(s, code string, noColor bool) string {
	if noColor {
		return s
	}
	return code + s + colorResetStr
}
