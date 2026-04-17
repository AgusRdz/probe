package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/AgusRdz/probe/config"
)

// RunConfig runs `probe config [show|edit [global|project]]`.
func RunConfig(args []string, cfg *config.Config) {
	sub := "show"
	if len(args) > 0 {
		sub = args[0]
	}

	switch sub {
	case "show":
		configShow(cfg)
	case "edit":
		target := "project"
		if len(args) > 1 {
			target = args[1]
		}
		configEdit(target, cfg)
	default:
		fmt.Fprintf(os.Stderr, "probe config: unknown subcommand %q\n\nusage: probe config [show|edit [global|project]]\n", sub)
		os.Exit(1)
	}
}

func configShow(cfg *config.Config) {
	globalPath := config.Path()
	projectPath := config.ProjectPath()

	_, globalExists := os.Stat(globalPath)
	_, projectExists := os.Stat(projectPath)

	fmt.Printf("Global config:  %s", globalPath)
	if os.IsNotExist(globalExists) {
		fmt.Print("  (not found)")
	}
	fmt.Println()

	fmt.Printf("Project config: %s", projectPath)
	if os.IsNotExist(projectExists) {
		fmt.Print("  (not found)")
	}
	fmt.Println()

	fmt.Println()
	editor := resolveEditor(cfg)
	fmt.Printf("Editor: %s\n", editor)
	if cfg.Output.Editor == "" {
		fmt.Println("  (set output.editor in global config or $PROBE_EDITOR to override)")
	}
	fmt.Println()
	fmt.Println("To edit:")
	fmt.Println("  probe config edit          # project (.probe.yml in cwd)")
	fmt.Println("  probe config edit global   # global (~/.config/probe/config.yml)")
}

// configSection is a top-level config block with its YAML key and commented template.
type configSection struct {
	key      string // top-level YAML key, e.g. "proxy"
	template string // commented-out template block
}

// configSections defines every supported top-level section in order.
// A section is appended to an existing config only when its key is absent.
var configSections = []configSection{
	{"proxy", `# proxy:
#   port: 4000
#   target: http://localhost:3001
#   bind: 127.0.0.1
#   filter: /api
#   ignore:
#     - /health
#     - /metrics
#   body_size_limit: 1048576   # 1MB
`},
	{"inference", `# inference:
#   path_normalization_threshold: 3    # calls before a segment becomes {id}
#   confidence_threshold: 0.9          # required vs optional field cutoff
#   max_xml_depth: 20
`},
	{"export", `# export:
#   default_format: openapi
#   min_calls: 0            # 0 = include scan-only; 1 = observed traffic only
#   info_title: "My API"
#   info_version: "1.0.0"
#   output_dir: ./exports   # all formats go here, auto-named (<dir>.<ext>)
#   outputs:                # per-format overrides — wins over output_dir
#     openapi: api.yaml
#     json:    api.json
#     swagger: swagger.yaml
#     postman: collection.json
#     curl:    api.sh
#     httpie:  api-httpie.sh
#     bruno:   ./my-api-bruno
`},
	{"list", `# list:
#   columns: method,path,source,file,calls,coverage
#   # available: method path source file calls coverage protocol status framework
`},
	{"output", `# output:
#   no_color: false
#   editor: code       # editor for 'probe config edit' (e.g. code, vim, nano, notepad++)
#                      # also settable via $PROBE_EDITOR env var
`},
	{"path_overrides", `# path_overrides:
#   - pattern: "/api/v*/users/me"
#     keep_as: "/api/v{version}/users/me"
`},
}

// configTemplate is the full file written when creating a new config from scratch.
var configTemplate = func() string {
	var b strings.Builder
	b.WriteString("# probe configuration\n")
	b.WriteString("# Full reference: https://github.com/AgusRdz/probe\n")
	for _, s := range configSections {
		b.WriteByte('\n')
		b.WriteString(s.template)
	}
	return b.String()
}()

func configEdit(target string, cfg *config.Config) {
	var path string
	switch target {
	case "global":
		path = config.Path()
	case "project":
		path = config.ProjectPath()
	default:
		fmt.Fprintf(os.Stderr, "probe config edit: unknown target %q — use 'global' or 'project'\n", target)
		os.Exit(1)
	}

	// Create file with full template if it doesn't exist yet.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			fmt.Fprintf(os.Stderr, "probe config: create dir: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(path, []byte(configTemplate), 0600); err != nil {
			fmt.Fprintf(os.Stderr, "probe config: create file: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Created %s\n", path)
	} else {
		// File exists — append any sections that are entirely absent.
		if added := appendMissingSections(path); added > 0 {
			fmt.Printf("Added %d new setting(s) to %s\n", added, path)
		}
	}

	editor := resolveEditor(cfg)
	fmt.Printf("Opening %s with %s\n", path, editor)

	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() != 0 {
			fmt.Fprintf(os.Stderr, "probe config: editor exited with error: %v\n", err)
			os.Exit(1)
		}
	}
}

// appendMissingSections reads the existing config file and appends any top-level
// sections whose key does not appear anywhere in the file (even as a comment).
// Returns the number of sections appended.
func appendMissingSections(path string) int {
	existing, err := os.ReadFile(path)
	if err != nil {
		return 0
	}

	var missing []configSection
	for _, s := range configSections {
		// A section is considered present if its key appears anywhere in the file
		// (e.g. "proxy:", "# proxy:", "  proxy:").
		if !bytes.Contains(existing, []byte(s.key+":")) {
			missing = append(missing, s)
		}
	}
	if len(missing) == 0 {
		return 0
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return 0
	}
	defer f.Close() //nolint:errcheck

	fmt.Fprintf(f, "\n# --- added by probe (new settings) ---\n")
	for _, s := range missing {
		fmt.Fprintf(f, "\n%s", s.template)
	}
	return len(missing)
}

// resolveEditor returns the editor to use:
// $PROBE_EDITOR → output.editor in config → platform default (notepad / nano / vi).
// Intentionally ignores $EDITOR and $VISUAL — those belong to the user's shell, not probe.
func resolveEditor(cfg *config.Config) string {
	if e := os.Getenv("PROBE_EDITOR"); e != "" {
		return e
	}
	if cfg != nil && cfg.Output.Editor != "" {
		return cfg.Output.Editor
	}
	if runtime.GOOS == "windows" {
		return "notepad"
	}
	if _, err := exec.LookPath("nano"); err == nil {
		return "nano"
	}
	return "vi"
}
