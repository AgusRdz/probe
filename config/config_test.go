package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLoadReturnsDefaults(t *testing.T) {
	// Run from a temp dir that has no .probe.yml; there is no global config.
	tmp := t.TempDir()
	orig, _ := os.Getwd()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	def := Default()
	if cfg.Proxy.Port != def.Proxy.Port {
		t.Errorf("Proxy.Port = %d, want %d", cfg.Proxy.Port, def.Proxy.Port)
	}
	if cfg.Proxy.Bind != def.Proxy.Bind {
		t.Errorf("Proxy.Bind = %q, want %q", cfg.Proxy.Bind, def.Proxy.Bind)
	}
	if cfg.Proxy.BodySizeLimit != def.Proxy.BodySizeLimit {
		t.Errorf("Proxy.BodySizeLimit = %d, want %d", cfg.Proxy.BodySizeLimit, def.Proxy.BodySizeLimit)
	}
	if cfg.Scan.Dir != def.Scan.Dir {
		t.Errorf("Scan.Dir = %q, want %q", cfg.Scan.Dir, def.Scan.Dir)
	}
	if cfg.Inference.ConfidenceThreshold != def.Inference.ConfidenceThreshold {
		t.Errorf("Inference.ConfidenceThreshold = %v, want %v",
			cfg.Inference.ConfidenceThreshold, def.Inference.ConfidenceThreshold)
	}
	if cfg.Export.DefaultFormat != def.Export.DefaultFormat {
		t.Errorf("Export.DefaultFormat = %q, want %q", cfg.Export.DefaultFormat, def.Export.DefaultFormat)
	}
	if cfg.Export.OpenAPIVersion != def.Export.OpenAPIVersion {
		t.Errorf("Export.OpenAPIVersion = %q, want %q", cfg.Export.OpenAPIVersion, def.Export.OpenAPIVersion)
	}
	if cfg.Output.JSONIndent != def.Output.JSONIndent {
		t.Errorf("Output.JSONIndent = %d, want %d", cfg.Output.JSONIndent, def.Output.JSONIndent)
	}
}

func TestProjectConfigOverridesGlobal(t *testing.T) {
	tmp := t.TempDir()

	// Global config
	globalCfgYAML := []byte(`
proxy:
  port: 9000
  bind: "0.0.0.0"
export:
  info_title: "Global API"
`)

	// Project config — overrides port and title, adds version
	projectCfgYAML := []byte(`
proxy:
  port: 5000
export:
  info_title: "Project API"
  info_version: "1.2.3"
`)

	globalFile := filepath.Join(tmp, "global.yml")
	projectFile := filepath.Join(tmp, ".probe.yml")
	if err := os.WriteFile(globalFile, globalCfgYAML, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectFile, projectCfgYAML, 0644); err != nil {
		t.Fatal(err)
	}

	// Build merged config manually using the same merge() logic as Load().
	cfg := Default()

	var global Config
	if data, err := os.ReadFile(globalFile); err == nil {
		if err := yaml.Unmarshal(data, &global); err != nil {
			t.Fatalf("unmarshal global: %v", err)
		}
		merge(cfg, &global)
	}

	var project Config
	if data, err := os.ReadFile(projectFile); err == nil {
		if err := yaml.Unmarshal(data, &project); err != nil {
			t.Fatalf("unmarshal project: %v", err)
		}
		merge(cfg, &project)
	}

	// Project port wins over global
	if cfg.Proxy.Port != 5000 {
		t.Errorf("Proxy.Port = %d, want 5000", cfg.Proxy.Port)
	}
	// Global bind survives (project didn't set it)
	if cfg.Proxy.Bind != "0.0.0.0" {
		t.Errorf("Proxy.Bind = %q, want 0.0.0.0", cfg.Proxy.Bind)
	}
	// Project title wins over global
	if cfg.Export.InfoTitle != "Project API" {
		t.Errorf("Export.InfoTitle = %q, want Project API", cfg.Export.InfoTitle)
	}
	// Project version set
	if cfg.Export.InfoVersion != "1.2.3" {
		t.Errorf("Export.InfoVersion = %q, want 1.2.3", cfg.Export.InfoVersion)
	}
}

func TestMissingConfigReturnsDefaults(t *testing.T) {
	// Load from a dir with no .probe.yml — must not error and must return defaults.
	tmp := t.TempDir()
	orig, _ := os.Getwd()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() with no config files returned error: %v", err)
	}
	if cfg == nil {
		t.Fatal("Load() returned nil")
	}
	if cfg.Proxy.Port != 4000 {
		t.Errorf("Proxy.Port = %d, want 4000", cfg.Proxy.Port)
	}
	if cfg.Inference.MaxXMLDepth != 20 {
		t.Errorf("Inference.MaxXMLDepth = %d, want 20", cfg.Inference.MaxXMLDepth)
	}
	if !cfg.Inference.ArrayMergeItems {
		t.Error("Inference.ArrayMergeItems should default to true")
	}
}
