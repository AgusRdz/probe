package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

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

// configTemplate is written to a new config file so the user has a reference.
const configTemplate = `# probe configuration
# Full reference: https://github.com/AgusRdz/probe

# proxy:
#   port: 4000
#   target: http://localhost:3001
#   bind: 127.0.0.1
#   filter: /api
#   ignore:
#     - /health
#     - /metrics
#   body_size_limit: 1048576   # 1MB

# inference:
#   path_normalization_threshold: 3    # calls before a segment becomes {id}
#   confidence_threshold: 0.9          # required vs optional field cutoff
#   max_xml_depth: 20

# export:
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

# list:
#   columns: method,path,source,file,calls,coverage
#   # available: method path source file calls coverage protocol status framework

# output:
#   no_color: false
#   editor: code       # editor for 'probe config edit' (e.g. code, vim, nano, notepad++)
#                      # also settable via $PROBE_EDITOR env var

# path_overrides:
#   - pattern: "/api/v*/users/me"
#     keep_as: "/api/v{version}/users/me"
`

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

	// Create file with template if it doesn't exist yet.
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
	}

	editor := resolveEditor(cfg)
	fmt.Printf("Opening %s with %s\n", path, editor)

	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// Some editors (code, subl) fork and return immediately — that's fine.
		// Only surface the error if the file was never opened at all.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() != 0 {
			fmt.Fprintf(os.Stderr, "probe config: editor exited with error: %v\n", err)
			os.Exit(1)
		}
	}
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
