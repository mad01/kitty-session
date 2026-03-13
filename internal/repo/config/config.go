package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const configFileName = "config.yaml"

// Layout constants for session window arrangement.
const (
	LayoutSplit = "split"
	LayoutTab   = "tab"
)

// Config holds the repo finder configuration.
type Config struct {
	Dirs   []string `yaml:"dirs"`
	Layout string   `yaml:"layout"`
}

// EffectiveLayout returns the configured layout, defaulting to split.
// Safe to call on a nil receiver.
func (c *Config) EffectiveLayout() string {
	if c != nil && c.Layout == LayoutTab {
		return LayoutTab
	}
	return LayoutSplit
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
