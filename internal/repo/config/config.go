// Package config loads ~/.config/ks/config.yaml, the one user-authored file
// ks reads: the repo roots for the picker, the scratch directory, and the
// settings of the ks-owned kitty instance.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	configFileName = "config.yaml"
	// defaultSocketFile is the instance's remote-control socket under ~/.config/ks/.
	defaultSocketFile = "kitty.sock"
	// socketScheme prefixes a socket path for kitty's --to and --listen-on flags.
	socketScheme = "unix:"
)

// Sidebar width bounds, in terminal cells.
const (
	// DefaultSidebarWidth applies when sidebar_width is unset.
	DefaultSidebarWidth = 36
	// MinSidebarWidth is the narrowest sidebar the TUI can still render.
	MinSidebarWidth = 20
)

// Config is the parsed config.yaml. A nil *Config behaves like an empty file,
// so callers that tolerate a missing config can use the accessors directly.
type Config struct {
	Dirs []string `yaml:"dirs"`
	// Layout and Summary shaped the topology before ks owned its kitty
	// instance. They are still parsed so old files load, and otherwise ignored.
	Layout  string `yaml:"layout"`
	Summary bool   `yaml:"summary"`
	TmpDir  string `yaml:"tmpdir"`
	// KittySocket is the path of the instance's remote-control socket.
	KittySocket string `yaml:"kitty_socket"`
	// SidebarWidth is the width of each session's sidebar window in cells.
	SidebarWidth int `yaml:"sidebar_width"`
	// KittyOverrides are extra key=value settings the instance starts with,
	// each passed to kitty as -o after the ones ks needs.
	KittyOverrides []string `yaml:"kitty_overrides"`
}

// EffectiveTmpDir returns the configured tmpdir for scratch sessions.
// Returns empty string when unset, meaning os.MkdirTemp default should be used.
func (c *Config) EffectiveTmpDir() string {
	if c != nil && c.TmpDir != "" {
		return expandTilde(c.TmpDir)
	}
	return ""
}

// Socket returns the instance's remote-control address in the form kitty's
// --to and --listen-on flags take: unix:<absolute path>. Unset, it is
// ~/.config/ks/kitty.sock.
func (c *Config) Socket() (string, error) {
	if c != nil && c.KittySocket != "" {
		return socketScheme + c.KittySocket, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return socketScheme + filepath.Join(home, ".config", "ks", defaultSocketFile), nil
}

// SocketPath returns the file path behind a Socket address.
func SocketPath(socket string) string {
	return strings.TrimPrefix(socket, socketScheme)
}

// EffectiveSidebarWidth returns the sidebar width in cells: the configured
// value raised to MinSidebarWidth, or DefaultSidebarWidth when unset.
func (c *Config) EffectiveSidebarWidth() int {
	if c == nil || c.SidebarWidth == 0 {
		return DefaultSidebarWidth
	}
	return max(c.SidebarWidth, MinSidebarWidth)
}

// Overrides returns the extra kitty settings for the instance, nil when none.
func (c *Config) Overrides() []string {
	if c == nil {
		return nil
	}
	return c.KittyOverrides
}

// Load reads config.yaml from ~/.config/ks/config.yaml.
func Load() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home directory: %w", err)
	}
	globalPath := filepath.Join(home, ".config", "ks", configFileName)
	cfg, err := loadFrom(globalPath)
	if err != nil {
		return nil, fmt.Errorf("no config found (checked %s): %w", globalPath, err)
	}
	return cfg, nil
}

// LoadFrom reads config from a specific path.
func LoadFrom(path string) (*Config, error) {
	return loadFrom(path)
}

func loadFrom(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	// Expand tildes in directory paths
	for i, d := range cfg.Dirs {
		cfg.Dirs[i] = expandTilde(d)
	}
	cfg.TmpDir = expandTilde(cfg.TmpDir)
	if cfg.KittySocket != "" {
		// A relative socket path resolves against the config file's directory
		// (~/.config/ks), never the process working directory, so ks reaches
		// the same instance whatever directory it is run from.
		sock := expandTilde(cfg.KittySocket)
		if !filepath.IsAbs(sock) {
			sock = filepath.Join(filepath.Dir(path), sock)
		}
		cfg.KittySocket = filepath.Clean(sock)
	}
	for i, o := range cfg.KittyOverrides {
		if !strings.Contains(o, "=") {
			return nil, fmt.Errorf(
				"parsing %s: kitty_overrides[%d] %q is not key=value",
				path,
				i,
				o,
			)
		}
	}

	return &cfg, nil
}

func expandTilde(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return p
		}
		return filepath.Join(home, p[2:])
	}
	return p
}
