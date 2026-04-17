package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/AgusRdz/probe/cmd"
	"github.com/AgusRdz/probe/config"
	"github.com/AgusRdz/probe/updater"
)

var version = "dev" // set by ldflags: -X main.version=v1.0.0

func main() {
	updater.NotifyIfUpdateAvailable(version)

	cfg, _ := config.Load() // never fatal on missing config

	cmd.Version = version

	if len(os.Args) < 2 {
		printHelp()
		os.Exit(0)
	}

	switch os.Args[1] {
	case "intercept":
		cmd.RunIntercept(os.Args[2:], cfg)
	case "list":
		cmd.RunList(os.Args[2:], cfg)
	case "show":
		cmd.RunShow(os.Args[2:], cfg)
	case "export":
		cmd.RunExport(os.Args[2:], cfg)
	case "annotate":
		cmd.RunAnnotate(os.Args[2:], cfg)
	case "stats":
		cmd.RunStats(os.Args[2:], cfg)
	case "clear":
		cmd.RunClear(os.Args[2:], cfg)
	case "scan":
		cmd.RunScan(os.Args[2:], cfg)
	case "update":
		cmd.RunUpdate(os.Args[2:])
	case "version", "--version", "-v":
		cmd.RunVersion(os.Args[2:])
	case "help", "--help", "-h":
		if len(os.Args) > 2 {
			printCommandHelp(os.Args[2])
		} else {
			printHelp()
		}
	default:
		fmt.Fprintf(os.Stderr, "probe: unknown command %q\n\nRun 'probe help' for usage.\n", os.Args[1])
		os.Exit(1)
	}
}

func printHelp() {
	const colW = 42
	section := func(name string) string { return bold(cyan(name)) + "\n" }
	row := func(cmd, desc string) string {
		return fmt.Sprintf("  %-*s%s\n", colW, cmd, dim(desc))
	}
	flag := func(f string) string { return yellow(f) }

	var b strings.Builder

	b.WriteString(fmt.Sprintf("%s %s — API endpoint discovery and documentation\n\n", bold("probe"), version))

	b.WriteString(bold("Usage") + "\n")
	b.WriteString(row("probe <command> [flags]", "Run a probe command"))
	b.WriteString("\n")

	b.WriteString(section("Traffic observation"))
	b.WriteString(row("intercept "+flag("--target <url>"), "Proxy traffic and capture endpoint schemas"))
	b.WriteString(row("  "+flag("--port <n>"), "Local port (default 4000)"))
	b.WriteString(row("  "+flag("--bind <addr>"), "Bind address (default 127.0.0.1)"))
	b.WriteString(row("  "+flag("--filter <prefix>"), "Only capture paths with this prefix"))
	b.WriteString(row("  "+flag("--ignore <paths>"), "Comma-separated path prefixes to skip"))
	b.WriteString(row("  "+flag("--db <path>"), "Override DB path"))
	b.WriteString("\n")

	b.WriteString(section("Discovery"))
	b.WriteString(row("list", "List all discovered endpoints"))
	b.WriteString(row("  "+flag("--json"), "Output as JSON"))
	b.WriteString(row("  "+flag("--min-calls <n>"), "Only endpoints with N+ calls"))
	b.WriteString(row("  "+flag("--source <src>"), "Filter: scan, observed, scan+obs"))
	b.WriteString(row("  "+flag("--protocol <p>"), "Filter: rest, graphql, grpc"))
	b.WriteString(row("  "+flag("--cols <cols>"), "Columns to show (default: method,path,source,file,calls,coverage)"))
	b.WriteString(row("", "  Available: method path source file calls coverage protocol status framework"))
	b.WriteString(row("", "  coverage = schema evidence bar (call count × source quality)"))
	b.WriteString(row("show <METHOD> <PATH>", "Full detail: schema + coverage breakdown"))
	b.WriteString(row("  "+flag("--calls"), "Show individual observations"))
	b.WriteString(row("  "+flag("--json"), "Output as JSON"))
	b.WriteString("\n")

	b.WriteString(section("Export"))
	b.WriteString(row("export", "Export as OpenAPI 3.x YAML"))
	b.WriteString(row("  "+flag("--format openapi"), "Output format (default: openapi)"))
	b.WriteString(row("  "+flag("--out <file>"), "Write to file instead of stdout"))
	b.WriteString(row("  "+flag("--min-confidence <f>"), "Minimum confidence threshold"))
	b.WriteString(row("  "+flag("--include-skeleton"), "Include scan-only endpoints"))
	b.WriteString("\n")

	b.WriteString(section("Annotation"))
	b.WriteString(row(`annotate "METHOD /path"`, "Add description, tags, or path override"))
	b.WriteString(row("  "+flag("--description <text>"), "Set description"))
	b.WriteString(row("  "+flag("--tag <tag>"), "Add tag (repeatable)"))
	b.WriteString(row("  "+flag("--path-override <pat>"), "Pin canonical path pattern"))
	b.WriteString("\n")

	b.WriteString(section("Maintenance"))
	b.WriteString(row("stats", "Show endpoint count summary"))
	b.WriteString(row("clear", "Delete all observations"))
	b.WriteString(row("  "+flag("--endpoint \"METHOD /path\""), "Delete one endpoint"))
	b.WriteString(row("  "+flag("--yes"), "Skip confirmation"))
	b.WriteString("\n")

	b.WriteString(section("Other"))
	b.WriteString(row("update", "Download and install the latest release"))
	b.WriteString(row("version", "Show version"))
	b.WriteString(row("help [command]", "Show this help or command help"))
	b.WriteString("\n")

	b.WriteString(bold("Config") + "\n")
	b.WriteString(dim(fmt.Sprintf("  global:  %s\n", config.Path())))
	b.WriteString(dim("  project: .probe.yml (walk up from cwd)\n"))
	b.WriteString("\n")

	b.WriteString(bold("Examples") + "\n")
	b.WriteString(row("probe intercept --target http://localhost:3001", "Start capturing traffic"))
	b.WriteString(row("probe list --source observed", "List observed endpoints"))
	b.WriteString(row("probe show GET /users --calls", "Inspect endpoint with observations"))
	b.WriteString(row("probe export --out openapi.yaml", "Export discovered spec"))

	fmt.Print(b.String())
}

func printCommandHelp(command string) {
	// Delegate to each subcommand's --help by passing --help as the first flag.
	// Each command uses flag.ExitOnError which will print usage and exit.
	switch command {
	case "intercept":
		cmd.RunIntercept([]string{"--help"}, &config.Config{})
	case "list":
		cmd.RunList([]string{"--help"}, &config.Config{})
	case "show":
		cmd.RunShow([]string{"--help"}, &config.Config{})
	case "export":
		cmd.RunExport([]string{"--help"}, &config.Config{})
	case "annotate":
		cmd.RunAnnotate([]string{"--help"}, &config.Config{})
	case "stats":
		cmd.RunStats([]string{"--help"}, &config.Config{})
	case "clear":
		cmd.RunClear([]string{"--help"}, &config.Config{})
	case "scan":
		cmd.RunScan([]string{"--help"}, &config.Config{})
	default:
		fmt.Fprintf(os.Stderr, "probe: unknown command %q\n\nRun 'probe help' for usage.\n", command)
		os.Exit(1)
	}
}
