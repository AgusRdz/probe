package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ProxyConfig controls the reverse proxy listener.
type ProxyConfig struct {
	Port          int    `yaml:"port"`
	Bind          string `yaml:"bind"`
	BodySizeLimit int    `yaml:"body_size_limit"`
}

// ScanConfig controls static-analysis scanning.
type ScanConfig struct {
	Dir        string   `yaml:"dir"`
	Frameworks []string `yaml:"frameworks"`
	Exclude    []string `yaml:"exclude"`
}

// InferenceConfig tunes schema inference behaviour.
type InferenceConfig struct {
	PathNormalizationThreshold int     `yaml:"path_normalization_threshold"`
	ConfidenceThreshold        float64 `yaml:"confidence_threshold"`
	MaxXMLDepth                int     `yaml:"max_xml_depth"`
	XMLPreserveNamespaces      bool    `yaml:"xml_preserve_namespaces"`
	ArrayMergeItems            bool    `yaml:"array_merge_items"`
}

// ExportConfig controls generated-output defaults.
type ExportConfig struct {
	DefaultFormat   string            `yaml:"default_format"`
	MinConfidence   float64           `yaml:"min_confidence"`
	IncludeSkeleton bool              `yaml:"include_skeleton"` // kept for YAML compat; use MinCalls instead
	MinCalls        int               `yaml:"min_calls"`        // 0 = include all; 1 = observed only
	OpenAPIVersion  string            `yaml:"openapi_version"`
	InfoTitle       string            `yaml:"info_title"`
	InfoVersion     string            `yaml:"info_version"`
	OutputDir       string            `yaml:"output_dir"` // base directory for all format outputs (auto-named)
	Outputs         map[string]string `yaml:"outputs"`    // per-format overrides (wins over output_dir)
}

// OutputConfig controls CLI presentation.
type OutputConfig struct {
	NoColor    bool   `yaml:"no_color"`
	JSONIndent int    `yaml:"json_indent"`
	Editor     string `yaml:"editor"` // editor for 'probe config edit'; overrides platform default
}

// ListConfig controls the probe list display.
type ListConfig struct {
	Columns string `yaml:"columns"` // comma-separated, e.g. "method,path,source,file,calls,confidence"
}

// PathOverride pins a normalised pattern to a fixed string.
type PathOverride struct {
	Pattern string `yaml:"pattern"`
	KeepAs  string `yaml:"keep_as"`
}

// Config is the top-level configuration structure.
type Config struct {
	Proxy         ProxyConfig     `yaml:"proxy"`
	Scan          ScanConfig      `yaml:"scan"`
	Inference     InferenceConfig `yaml:"inference"`
	Export        ExportConfig    `yaml:"export"`
	Output        OutputConfig    `yaml:"output"`
	List          ListConfig      `yaml:"list"`
	PathOverrides []PathOverride  `yaml:"path_overrides"`
}

// Default returns a *Config populated with all built-in defaults.
func Default() *Config {
	return &Config{
		Proxy: ProxyConfig{
			Port:          4000,
			Bind:          "127.0.0.1",
			BodySizeLimit: 1048576,
		},
		Scan: ScanConfig{
			Dir:        "./",
			Frameworks: []string{},
			Exclude: []string{
				"**/node_modules/**",
				"**/vendor/**",
				"**/__pycache__/**",
				"**/bin/**",
				"**/obj/**",
				"**/.git/**",
			},
		},
		Inference: InferenceConfig{
			PathNormalizationThreshold: 3,
			ConfidenceThreshold:        0.9,
			MaxXMLDepth:                20,
			XMLPreserveNamespaces:      false,
			ArrayMergeItems:            true,
		},
		Export: ExportConfig{
			DefaultFormat:   "openapi",
			MinConfidence:   0.0,
			IncludeSkeleton: true,
			MinCalls:        0,
			OpenAPIVersion:  "3.0.3",
			InfoTitle:       "Discovered API",
			InfoVersion:     "0.0.1",
			Outputs:         map[string]string{},
		},
		Output: OutputConfig{
			NoColor:    false,
			JSONIndent: 2,
		},
		List: ListConfig{
			Columns: "method,path,source,file,calls,coverage",
		},
		PathOverrides: []PathOverride{},
	}
}

// Path returns the global config file path (~/.config/probe/config.yml).
func Path() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "probe", "config.yml")
}

// ProjectPath returns the path of the nearest .probe.yml walking up from cwd,
// or ".probe.yml" in cwd if none exists yet.
func ProjectPath() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ".probe.yml"
	}
	dir := cwd
	for {
		p := filepath.Join(dir, ".probe.yml")
		if _, err := os.Stat(p); err == nil {
			return p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return filepath.Join(cwd, ".probe.yml")
}

// Load returns a *Config with defaults, overlaid by the global config (if
// present) and then the nearest .probe.yml walking up from cwd.
// A missing config file is not an error — defaults are returned silently.
func Load() (*Config, error) {
	cfg := Default()

	// Global config: ~/.config/probe/config.yml
	if data, err := os.ReadFile(Path()); err == nil {
		var global Config
		if err := yaml.Unmarshal(data, &global); err != nil {
			return nil, fmt.Errorf("global config: %w", err)
		}
		merge(cfg, &global)
	}

	// Project config: walk up from cwd looking for .probe.yml
	cwd, err := os.Getwd()
	if err == nil {
		dir := cwd
		for {
			projectPath := filepath.Join(dir, ".probe.yml")
			if data, err := os.ReadFile(projectPath); err == nil {
				var project Config
				if err := yaml.Unmarshal(data, &project); err != nil {
					return nil, fmt.Errorf("project config (%s): %w", projectPath, err)
				}
				merge(cfg, &project)
				break
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	return cfg, nil
}

// merge overlays non-zero fields from override onto base.
func merge(base, override *Config) {
	// Proxy
	if override.Proxy.Port != 0 {
		base.Proxy.Port = override.Proxy.Port
	}
	if override.Proxy.Bind != "" {
		base.Proxy.Bind = override.Proxy.Bind
	}
	if override.Proxy.BodySizeLimit != 0 {
		base.Proxy.BodySizeLimit = override.Proxy.BodySizeLimit
	}

	// Scan
	if override.Scan.Dir != "" {
		base.Scan.Dir = override.Scan.Dir
	}
	if len(override.Scan.Frameworks) > 0 {
		base.Scan.Frameworks = override.Scan.Frameworks
	}
	if len(override.Scan.Exclude) > 0 {
		base.Scan.Exclude = override.Scan.Exclude
	}

	// Inference
	if override.Inference.PathNormalizationThreshold != 0 {
		base.Inference.PathNormalizationThreshold = override.Inference.PathNormalizationThreshold
	}
	if override.Inference.ConfidenceThreshold != 0 {
		base.Inference.ConfidenceThreshold = override.Inference.ConfidenceThreshold
	}
	if override.Inference.MaxXMLDepth != 0 {
		base.Inference.MaxXMLDepth = override.Inference.MaxXMLDepth
	}
	if override.Inference.XMLPreserveNamespaces {
		base.Inference.XMLPreserveNamespaces = true
	}
	// ArrayMergeItems: explicit false in override means "turn it off"
	// We use a pointer-less approach: only override when the field is explicitly
	// set in YAML. Since bool zero is false, we cannot distinguish "not set" from
	// "set to false" here, so we leave ArrayMergeItems as-is when override is
	// false — which means project configs can only enable it, not disable it.
	if override.Inference.ArrayMergeItems {
		base.Inference.ArrayMergeItems = true
	}

	// Export
	if override.Export.DefaultFormat != "" {
		base.Export.DefaultFormat = override.Export.DefaultFormat
	}
	if override.Export.MinConfidence != 0 {
		base.Export.MinConfidence = override.Export.MinConfidence
	}
	if override.Export.IncludeSkeleton {
		base.Export.IncludeSkeleton = true
	}
	if override.Export.OpenAPIVersion != "" {
		base.Export.OpenAPIVersion = override.Export.OpenAPIVersion
	}
	if override.Export.InfoTitle != "" {
		base.Export.InfoTitle = override.Export.InfoTitle
	}
	if override.Export.InfoVersion != "" {
		base.Export.InfoVersion = override.Export.InfoVersion
	}
	if override.Export.MinCalls != 0 {
		base.Export.MinCalls = override.Export.MinCalls
	}
	if override.Export.OutputDir != "" {
		base.Export.OutputDir = override.Export.OutputDir
	}
	for k, v := range override.Export.Outputs {
		if base.Export.Outputs == nil {
			base.Export.Outputs = make(map[string]string)
		}
		base.Export.Outputs[k] = v
	}

	// Output
	if override.Output.NoColor {
		base.Output.NoColor = true
	}
	if override.Output.JSONIndent != 0 {
		base.Output.JSONIndent = override.Output.JSONIndent
	}
	if override.Output.Editor != "" {
		base.Output.Editor = override.Output.Editor
	}

	// List
	if override.List.Columns != "" {
		base.List.Columns = override.List.Columns
	}

	// PathOverrides: project appends to global
	if len(override.PathOverrides) > 0 {
		base.PathOverrides = append(base.PathOverrides, override.PathOverrides...)
	}
}
