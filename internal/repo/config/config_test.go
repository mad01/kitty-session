package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFrom(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yaml")

	content := []byte("dirs:\n  - /tmp/repos\n  - /tmp/other\n")
	if err := os.WriteFile(cfgPath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	if len(cfg.Dirs) != 2 {
		t.Fatalf("expected 2 dirs, got %d", len(cfg.Dirs))
	}
	if cfg.Dirs[0] != "/tmp/repos" {
		t.Errorf("expected /tmp/repos, got %s", cfg.Dirs[0])
	}
	if cfg.Dirs[1] != "/tmp/other" {
		t.Errorf("expected /tmp/other, got %s", cfg.Dirs[1])
	}
}

func TestLoadFromTildeExpansion(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yaml")

	content := []byte("dirs:\n  - ~/code/repos\n")
	if err := os.WriteFile(cfgPath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, "code/repos")
	if cfg.Dirs[0] != expected {
		t.Errorf("expected %s, got %s", expected, cfg.Dirs[0])
	}
}

func TestLoadFromMissingFile(t *testing.T) {
	_, err := LoadFrom("/nonexistent/config.yaml")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadFromInvalidYAML(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yaml")

	content := []byte("not: [valid: yaml: {{{\n")
	if err := os.WriteFile(cfgPath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFrom(cfgPath)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestLoadFromGlobalConfig(t *testing.T) {
	tmp := t.TempDir()

	// Set HOME to tmp so Load() looks in tmp/.config/ks/
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmp)
	defer os.Setenv("HOME", origHome)

	cfgDir := filepath.Join(tmp, ".config", "ks")
	os.MkdirAll(cfgDir, 0o755)
	cfgPath := filepath.Join(cfgDir, "config.yaml")

	content := []byte("dirs:\n  - /global/repos\n")
	if err := os.WriteFile(cfgPath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Dirs[0] != "/global/repos" {
		t.Errorf("expected /global/repos from global config, got %s", cfg.Dirs[0])
	}
}
