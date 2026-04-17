package render

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/AgusRdz/probe/store"
)

// Column name constants.
const (
	ColMethod    = "method"
	ColPath      = "path"
	ColSource    = "source"
	ColFile      = "file" // basename:line from SourceFile/SourceLine
	ColCalls     = "calls"
	ColCoverage  = "coverage"   // schema coverage bar — evidence strength (call count × source quality)
	ColConfidence = "confidence" // alias for coverage (backward compat)
	ColProtocol  = "protocol"
	ColStatus    = "status"
	ColFramework = "framework"
)

// DefaultColumns is the column list used when none is configured.
var DefaultColumns = []string{ColMethod, ColPath, ColSource, ColFile, ColCalls, ColCoverage}

// TableOptions controls table rendering.
type TableOptions struct {
	NoColor  bool
	JSON     bool   // if true, render as JSON array instead of table
	MinCalls int    // 0 = show all
	Source   string // "" = all, "scan", "observed", "scan+obs"
	Protocol string // "" = all, "rest", "graphql", "grpc", etc.
	Columns  []string // active columns in order; nil = DefaultColumns
}

// PrintTable writes the endpoint list to w.
// Output format is column-driven based on opts.Columns (or DefaultColumns).
//
// statusCodes maps endpointID → sorted status codes. Pass nil for fast mode (shows "—").
func PrintTable(w io.Writer, endpoints []store.Endpoint, statusCodes map[int64][]int, opts TableOptions) {
	filtered := filterEndpoints(endpoints, opts)
	if len(filtered) == 0 {
		fmt.Fprintln(w, colorize("  No endpoints found.", colorDimStr, opts.NoColor))
		return
	}

	// Resolve active columns.
	activeCols := opts.Columns
	if len(activeCols) == 0 {
		activeCols = DefaultColumns
	}

	// colHeader returns the display header for a column name.
	colHeader := func(col string) string {
		switch col {
		case ColMethod:
			return "METHOD"
		case ColPath:
			return "PATH"
		case ColSource:
			return "SOURCE"
		case ColFile:
			return "FILE"
		case ColCalls:
			return "CALLS"
		case ColCoverage, ColConfidence:
			return "COVERAGE"
		case ColProtocol:
			return "PROTOCOL"
		case ColStatus:
			return "STATUS CODES"
		case ColFramework:
			return "FRAMEWORK"
		default:
			return strings.ToUpper(col)
		}
	}

	type rowData struct {
		// plain values for width calculation
		method    string
		path      string
		source    string
		file      string
		calls     string
		conf      string // bar + pct, plain (no ANSI)
		protocol  string
		status    string
		framework string
		note      string
		confVal   float64
	}

	// Compute column widths initialised from header lengths.
	widths := make(map[string]int, len(activeCols))
	for _, col := range activeCols {
		widths[col] = len(colHeader(col))
	}

	rows := make([]rowData, 0, len(filtered))
	for _, ep := range filtered {
		conf := endpointConfidence(ep)
		bar := confidenceBar(conf)
		pct := confidencePct(conf)
		confPlain := bar + " " + pct

		sc := statusCodesStr(ep, statusCodes)

		var fileVal string
		if ep.SourceFile != "" {
			fileVal = filepath.Base(ep.SourceFile) + ":" + strconv.Itoa(ep.SourceLine)
		}

		var note string
		if conf < 0.30 && ep.CallCount > 0 {
			note = "← low coverage"
		} else if ep.CallCount == 0 {
			note = "← not yet seen"
		}

		r := rowData{
			method:    ep.Method,
			path:      ep.PathPattern,
			source:    ep.Source,
			file:      fileVal,
			calls:     fmt.Sprintf("%d", ep.CallCount),
			conf:      confPlain,
			protocol:  ep.Protocol,
			status:    sc,
			framework: ep.Framework,
			note:      note,
			confVal:   conf,
		}
		rows = append(rows, r)

		// Update widths.
		plainVal := func(col string) int {
			switch col {
			case ColMethod:
				return len(r.method)
			case ColPath:
				return len(r.path)
			case ColSource:
				return len(r.source)
			case ColFile:
				return len(r.file)
			case ColCalls:
				return len(r.calls)
			case ColCoverage, ColConfidence:
				return len(r.conf)
			case ColProtocol:
				return len(r.protocol)
			case ColStatus:
				return len(r.status)
			case ColFramework:
				return len(r.framework)
			default:
				return 0
			}
		}
		for _, col := range activeCols {
			if v := plainVal(col); v > widths[col] {
				widths[col] = v
			}
		}
	}

	// Build and print header line.
	var hbuf strings.Builder
	hbuf.WriteString("  ")
	for i, col := range activeCols {
		h := colHeader(col)
		if i < len(activeCols)-1 {
			fmt.Fprintf(&hbuf, "%-*s  ", widths[col], h)
		} else {
			hbuf.WriteString(h)
		}
	}
	headerLine := hbuf.String()
	fmt.Fprintln(w, colorize(headerLine, colorDimStr, opts.NoColor))

	// Print separator.
	sep := "  " + strings.Repeat("─", len(headerLine)-2)
	fmt.Fprintln(w, colorize(sep, colorDimStr, opts.NoColor))

	// Print rows.
	for _, r := range rows {
		var lbuf strings.Builder
		lbuf.WriteString("  ")

		for i, col := range activeCols {
			isLast := i == len(activeCols)-1
			w := widths[col]

			switch col {
			case ColSource:
				// Color with ANSI, pad based on plain width.
				colored := colorizeSource(r.source, opts.NoColor)
				padding := strings.Repeat(" ", w-len(r.source))
				lbuf.WriteString(colored + padding)
			case ColCoverage, ColConfidence:
				// Color with ANSI, pad based on plain width.
				colored := confidenceBarColored(r.confVal, opts.NoColor)
				padding := strings.Repeat(" ", w-len(r.conf))
				lbuf.WriteString(colored + padding)
			default:
				var plain string
				switch col {
				case ColMethod:
					plain = r.method
				case ColPath:
					plain = r.path
				case ColFile:
					plain = r.file
				case ColCalls:
					plain = r.calls
				case ColProtocol:
					plain = r.protocol
				case ColStatus:
					plain = r.status
				case ColFramework:
					plain = r.framework
				}
				fmt.Fprintf(&lbuf, "%-*s", w, plain)
			}

			if !isLast {
				lbuf.WriteString("  ")
			}
		}

		if r.note != "" {
			lbuf.WriteString("  ")
			lbuf.WriteString(colorize(r.note, colorDimStr, opts.NoColor))
		}

		fmt.Fprintln(w, lbuf.String())
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
