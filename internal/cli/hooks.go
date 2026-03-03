package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

// hookHandler represents a single hook command in the new Claude settings format.
type hookHandler struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// matcherGroup represents a matcher + hooks pair in the new format.
type matcherGroup struct {
	Matcher string        `json:"matcher"`
	Hooks   []hookHandler `json:"hooks"`
}

// shortenHome replaces the $HOME prefix with ~ for portability across systems.
func shortenHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}

// ksMatcherGroups returns the matcher groups that ks manages, keyed by event.
func ksMatcherGroups(binary string) map[string]matcherGroup {
	cmd := shortenHome(binary) + " _hook"
	handler := []hookHandler{{Type: "command", Command: cmd}}
	return map[string]matcherGroup{
		"PreToolUse":   {Matcher: ".*", Hooks: handler},
		"Stop":         {Matcher: "", Hooks: handler},
		"Notification": {Matcher: "permission_prompt|elicitation_dialog", Hooks: handler},
		"SessionStart": {Matcher: "", Hooks: handler},
	}
}


// ksHookCommands returns both the portable (~/) and absolute forms of the hook command
// so we can clean up entries written by either version.
func ksHookCommands(binary string) []string {
	short := shortenHome(binary) + " _hook"
	abs := binary + " _hook"
	if short == abs {
		return []string{short}
	}
	return []string{short, abs}
}

// isKsGroupAny checks if a matcher group contains any of the given hook commands.
func isKsGroupAny(mg matcherGroup, cmds []string) bool {
	for _, h := range mg.Hooks {
		for _, c := range cmds {
			if h.Command == c {
				return true
			}
		}
	}
	return false
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
	cmds := ksHookCommands(binary)

	for event, mg := range ksMatcherGroups(binary) {
		existing := hooks[event]
		// Remove any existing ks groups for this event (old or new path format)
		var kept []matcherGroup
		for _, g := range existing {
			if !isKsGroupAny(g, cmds) {
				kept = append(kept, g)
			}
		}
		hooks[event] = append(kept, mg)
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
	cmds := ksHookCommands(binary)

	for event, groups := range hooks {
		var kept []matcherGroup
		for _, g := range groups {
			if !isKsGroupAny(g, cmds) {
				kept = append(kept, g)
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
func getOrCreateHooksMap(settings map[string]any) map[string][]matcherGroup {
	hooks := make(map[string][]matcherGroup)

	raw, ok := settings["hooks"]
	if !ok {
		return hooks
	}

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
			mg := matcherGroup{}
			if m, ok := obj["matcher"].(string); ok {
				mg.Matcher = m
			}
			if hooksArr, ok := obj["hooks"].([]any); ok {
				for _, h := range hooksArr {
					hObj, ok := h.(map[string]any)
					if !ok {
						continue
					}
					hh := hookHandler{}
					if t, ok := hObj["type"].(string); ok {
						hh.Type = t
					}
					if c, ok := hObj["command"].(string); ok {
						hh.Command = c
					}
					mg.Hooks = append(mg.Hooks, hh)
				}
			}
			hooks[event] = append(hooks[event], mg)
		}
	}

	return hooks
}
