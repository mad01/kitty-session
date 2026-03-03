package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var hooksCmd = &cobra.Command{
	Use:   "hooks",
	Short: "Manage Claude Code hooks for state detection",
}

var hooksInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install ks hooks into Claude Code settings",
	RunE:  runHooksInstall,
}

var hooksUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove ks hooks from Claude Code settings",
	RunE:  runHooksUninstall,
}

func init() {
	hooksCmd.AddCommand(hooksInstallCmd)
	hooksCmd.AddCommand(hooksUninstallCmd)
	rootCmd.AddCommand(hooksCmd)
}

// settingsPath returns the path to ~/.claude/settings.json.
func settingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

// hookEntry represents a single hook command in the Claude settings.
type hookEntry struct {
	Matcher string `json:"matcher"`
	Command string `json:"command"`
}

// ksHookEntries returns the hook entries that ks manages.
func ksHookEntries(binary string) map[string][]hookEntry {
	cmd := binary + " _hook"
	return map[string][]hookEntry{
		"PreToolUse": {
			{Matcher: "*", Command: cmd},
		},
		"Stop": {
			{Matcher: "", Command: cmd},
		},
		"Notification": {
			{Matcher: "", Command: cmd},
		},
		"SessionStart": {
			{Matcher: "", Command: cmd},
		},
	}
}

func runHooksInstall(cmd *cobra.Command, args []string) error {
	binary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine ks binary path: %w", err)
	}

	path, err := settingsPath()
	if err != nil {
		return err
	}

	settings, err := readSettings(path)
	if err != nil {
		return err
	}

	hooks := getOrCreateHooksMap(settings)
	for event, entries := range ksHookEntries(binary) {
		hooks[event] = mergeHookEntries(hooks[event], entries, binary)
	}
	settings["hooks"] = hooks

	if err := writeSettings(path, settings); err != nil {
		return err
	}

	fmt.Fprintln(cmd.OutOrStdout(), "hooks installed")
	return nil
}

func runHooksUninstall(cmd *cobra.Command, args []string) error {
	binary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine ks binary path: %w", err)
	}

	path, err := settingsPath()
	if err != nil {
		return err
	}

	settings, err := readSettings(path)
	if err != nil {
		return err
	}

	hooks := getOrCreateHooksMap(settings)
	hookCmd := binary + " _hook"

	for event, existing := range hooks {
		var kept []hookEntry
		for _, e := range existing {
			if e.Command != hookCmd {
				kept = append(kept, e)
			}
		}
		if len(kept) > 0 {
			hooks[event] = kept
		} else {
			delete(hooks, event)
		}
	}

	if len(hooks) > 0 {
		settings["hooks"] = hooks
	} else {
		delete(settings, "hooks")
	}

	if err := writeSettings(path, settings); err != nil {
		return err
	}

	fmt.Fprintln(cmd.OutOrStdout(), "hooks uninstalled")
	return nil
}

// readSettings loads the Claude settings file, returning an empty map if it doesn't exist.
func readSettings(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]any), nil
		}
		return nil, fmt.Errorf("cannot read settings: %w", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("cannot parse settings: %w", err)
	}
	return settings, nil
}

// writeSettings marshals settings and writes them atomically.
func writeSettings(path string, settings map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("cannot create settings directory: %w", err)
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot marshal settings: %w", err)
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
}

// getOrCreateHooksMap extracts or creates the "hooks" map from settings.
func getOrCreateHooksMap(settings map[string]any) map[string][]hookEntry {
	hooks := make(map[string][]hookEntry)

	raw, ok := settings["hooks"]
	if !ok {
		return hooks
	}

	// settings["hooks"] is map[string]any from JSON unmarshal
	rawMap, ok := raw.(map[string]any)
	if !ok {
		return hooks
	}

	for event, val := range rawMap {
		arr, ok := val.([]any)
		if !ok {
			continue
		}
		for _, item := range arr {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			e := hookEntry{}
			if m, ok := obj["matcher"].(string); ok {
				e.Matcher = m
			}
			if c, ok := obj["command"].(string); ok {
				e.Command = c
			}
			hooks[event] = append(hooks[event], e)
		}
	}

	return hooks
}

// mergeHookEntries adds new entries to existing, replacing any with the same command prefix.
func mergeHookEntries(existing, new []hookEntry, binary string) []hookEntry {
	hookCmd := binary + " _hook"

	// Remove existing ks entries
	var kept []hookEntry
	for _, e := range existing {
		if e.Command != hookCmd {
			kept = append(kept, e)
		}
	}

	return append(kept, new...)
}
