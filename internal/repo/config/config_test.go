package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfig writes body as a config.yaml in a temp dir and returns its path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// load parses body and fails the test on error.
func load(t *testing.T, body string) *Config {
	t.Helper()
	cfg, err := LoadFrom(writeConfig(t, body))
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	return cfg
}

func TestLoadFrom(t *testing.T) {
	cfg := load(t, "dirs:\n  - /tmp/repos\n  - /tmp/other\n")
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
	cfg := load(t, "dirs:\n  - ~/code/repos\n")
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
	_, err := LoadFrom(writeConfig(t, "not: [valid: yaml: {{{\n"))
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

// TestLoadFromLegacyKeys: layout and summary no longer do anything, but a
// config written for an older ks must still load.
func TestLoadFromLegacyKeys(t *testing.T) {
	cfg := load(t, "dirs:\n  - /tmp/repos\nlayout: tab\nsummary: true\n")
	if cfg.Layout != "tab" || !cfg.Summary {
		t.Errorf("legacy keys not parsed: layout %q summary %v", cfg.Layout, cfg.Summary)
	}
}

func TestSocket(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	defaultSock := "unix:" + filepath.Join(home, ".config", "ks", "kitty.sock")

	tests := []struct {
		name string
		cfg  *Config // nil means "no config file"
		yaml string
		want string
	}{
		{"nil config uses the default", nil, "", defaultSock},
		{"unset uses the default", nil, "dirs:\n  - /tmp\n", defaultSock},
		{"absolute path is prefixed", nil, "kitty_socket: /run/ks.sock\n", "unix:/run/ks.sock"},
		{
			"tilde is expanded",
			nil,
			"kitty_socket: ~/.cache/ks.sock\n",
			"unix:" + filepath.Join(home, ".cache", "ks.sock"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg
			if tc.yaml != "" {
				cfg = load(t, tc.yaml)
			}
			got, err := cfg.Socket()
			if err != nil {
				t.Fatalf("Socket: %v", err)
			}
			if got != tc.want {
				t.Errorf("Socket() = %q, want %q", got, tc.want)
			}
			if SocketPath(got) != strings.TrimPrefix(tc.want, "unix:") {
				t.Errorf("SocketPath(%q) = %q", got, SocketPath(got))
			}
		})
	}
}

func TestEffectiveSidebarWidth(t *testing.T) {
	tests := []struct {
		name string
		yaml string // "" means nil config
		want int
	}{
		{"nil config", "", DefaultSidebarWidth},
		{"unset", "dirs:\n  - /tmp\n", DefaultSidebarWidth},
		{"configured", "sidebar_width: 48\n", 48},
		{"below the minimum is raised", "sidebar_width: 5\n", MinSidebarWidth},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var cfg *Config
			if tc.yaml != "" {
				cfg = load(t, tc.yaml)
			}
			if got := cfg.EffectiveSidebarWidth(); got != tc.want {
				t.Errorf("EffectiveSidebarWidth() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestOverrides(t *testing.T) {
	var nilCfg *Config
	if got := nilCfg.Overrides(); got != nil {
		t.Errorf("nil config Overrides() = %v, want nil", got)
	}
	cfg := load(t, "kitty_overrides:\n  - font_size=13\n  - background_opacity=0.9\n")
	want := []string{"font_size=13", "background_opacity=0.9"}
	if got := cfg.Overrides(); len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("Overrides() = %v, want %v", got, want)
	}
}

func TestOverridesRejectNonAssignment(t *testing.T) {
	_, err := LoadFrom(writeConfig(t, "kitty_overrides:\n  - font_size\n"))
	if err == nil || !strings.Contains(err.Error(), "key=value") {
		t.Fatalf("err = %v, want a key=value complaint", err)
	}
}

func TestEffectiveTmpDirDefault(t *testing.T) {
	cfg := load(t, "dirs:\n  - /tmp/repos\n")
	if got := cfg.EffectiveTmpDir(); got != "" {
		t.Errorf("expected empty string for default tmpdir, got %q", got)
	}
}

func TestEffectiveTmpDirCustom(t *testing.T) {
	cfg := load(t, "dirs:\n  - /tmp/repos\ntmpdir: /custom/workspaces\n")
	if got := cfg.EffectiveTmpDir(); got != "/custom/workspaces" {
		t.Errorf("expected /custom/workspaces, got %q", got)
	}
}

func TestEffectiveTmpDirTilde(t *testing.T) {
	cfg := load(t, "dirs:\n  - /tmp/repos\ntmpdir: ~/.config/ks/workspaces\n")
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".config/ks/workspaces")
	if got := cfg.EffectiveTmpDir(); got != expected {
		t.Errorf("expected %s, got %s", expected, got)
	}
}

func TestEffectiveTmpDirNilConfig(t *testing.T) {
	var cfg *Config
	if got := cfg.EffectiveTmpDir(); got != "" {
		t.Errorf("expected empty string for nil config, got %q", got)
	}
}

func TestLoadFromGlobalConfig(t *testing.T) {
	tmp := t.TempDir()

	// Set HOME to tmp so Load() looks in tmp/.config/ks/
	t.Setenv("HOME", tmp)

	cfgDir := filepath.Join(tmp, ".config", "ks")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
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

func TestRelativeSocketResolvesAgainstConfigDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("kitty_socket: ks.sock\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	got, err := cfg.Socket()
	if err != nil {
		t.Fatalf("Socket: %v", err)
	}
	want := "unix:" + filepath.Join(dir, "ks.sock")
	if got != want {
		t.Errorf("Socket() = %q, want %q (not resolved against cwd)", got, want)
	}
}
