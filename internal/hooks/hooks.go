// Package hooks edits the Claude Code hook registration ks relies on for state
// detection: the matcher groups in ~/.claude/settings.json that run `ks _hook`
// for the events in Events. Install and Uninstall rewrite the file, keeping
// every entry that is not a ks hook; Installed reads it.
package hooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// SessionEnd reasons that mean the user stopped the agent on purpose. The
// others (clear, resume, other) are restarts or the window going away. The
// SessionEnd matcher registers only these two; the handler checks again.
const (
	ReasonPromptInputExit = "prompt_input_exit"
	ReasonLogout          = "logout"
)

// hookSuffix ends every ks hook command, whichever binary path precedes it.
const hookSuffix = " _hook"

// Events are the Claude Code hook events ks registers, in reporting order.
var Events = []string{"PreToolUse", "Stop", "Notification", "SessionStart", "SessionEnd"}

// matchers is the matcher each event's ks group carries.
var matchers = map[string]string{
	"PreToolUse":   ".*",
	"Stop":         "",
	"Notification": "permission_prompt|elicitation_dialog",
	"SessionStart": "",
	"SessionEnd":   ReasonPromptInputExit + "|" + ReasonLogout,
}

// handler is one hook command in the Claude Code settings format.
type handler struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// group is a matcher with the handlers it runs.
type group struct {
	Matcher string    `json:"matcher"`
	Hooks   []handler `json:"hooks"`
}

// Path returns the Claude Code settings file, ~/.claude/settings.json.
func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

// Install registers a ks group for every event in Events, replacing any ks
// group already there (from this or an older binary path), and writes the
// settings back. Entries of other tools are kept.
func Install(path, binary string) error {
	settings, err := read(path)
	if err != nil {
		return err
	}
	hooks := groups(settings)
	cmds := commands(binary)
	ks := handler{Type: "command", Command: cmds[0]}
	for _, event := range Events {
		kept := without(hooks[event], cmds)
		hooks[event] = append(kept, group{Matcher: matchers[event], Hooks: []handler{ks}})
	}
	settings["hooks"] = hooks
	return write(path, settings)
}

// Uninstall removes every ks group for binary (portable or absolute form).
// Events left without groups are dropped, and so is an empty hooks key.
func Uninstall(path, binary string) error {
	settings, err := read(path)
	if err != nil {
		return err
	}
	hooks := groups(settings)
	cmds := commands(binary)
	for event, gs := range hooks {
		if kept := without(gs, cmds); len(kept) > 0 {
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
	return write(path, settings)
}

// Installed returns the events in Events that have a ks hook registered, in
// Events order. Any command ending in " _hook" counts, whichever ks binary it
// names: the hook writes the same state file from every build. A missing
// settings file means none.
func Installed(path string) ([]string, error) {
	settings, err := read(path)
	if err != nil {
		return nil, err
	}
	hooks := groups(settings)
	var installed []string
	for _, event := range Events {
		for _, g := range hooks[event] {
			if isKS(g, nil) {
				installed = append(installed, event)
				break
			}
		}
	}
	return installed, nil
}

// commands returns the hook command for binary in its portable form ($HOME
// shortened to ~, the one Install writes) and, when different, its absolute
// form, so entries written by either are recognized.
func commands(binary string) []string {
	short := shortenHome(binary) + hookSuffix
	abs := binary + hookSuffix
	if short == abs {
		return []string{short}
	}
	return []string{short, abs}
}

// shortenHome replaces the $HOME prefix with ~ for portability across machines.
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

// isKS reports whether g runs one of cmds, or with nil cmds any ks hook.
func isKS(g group, cmds []string) bool {
	for _, h := range g.Hooks {
		if cmds == nil && strings.HasSuffix(h.Command, hookSuffix) {
			return true
		}
		for _, c := range cmds {
			if h.Command == c {
				return true
			}
		}
	}
	return false
}

// without returns gs minus the groups that run one of cmds.
func without(gs []group, cmds []string) []group {
	var kept []group
	for _, g := range gs {
		if !isKS(g, cmds) {
			kept = append(kept, g)
		}
	}
	return kept
}

// read loads the settings file; a missing file is an empty settings object.
func read(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("cannot read settings: %w", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("cannot parse settings: %w", err)
	}
	if settings == nil {
		settings = map[string]any{}
	}
	return settings, nil
}

// write marshals settings pretty-printed with a trailing newline.
func write(path string, settings map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("cannot create settings directory: %w", err)
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot marshal settings: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("cannot write settings: %w", err)
	}
	return nil
}

// groups extracts the hooks map from settings as typed groups, skipping
// entries that do not have the expected shape.
func groups(settings map[string]any) map[string][]group {
	hooks := map[string][]group{}
	raw, ok := settings["hooks"].(map[string]any)
	if !ok {
		return hooks
	}
	for event, val := range raw {
		arr, ok := val.([]any)
		if !ok {
			continue
		}
		for _, item := range arr {
			if g, ok := parseGroup(item); ok {
				hooks[event] = append(hooks[event], g)
			}
		}
	}
	return hooks
}

// parseGroup reads one matcher group out of its generic JSON form.
func parseGroup(item any) (group, bool) {
	obj, ok := item.(map[string]any)
	if !ok {
		return group{}, false
	}
	g := group{}
	if m, ok := obj["matcher"].(string); ok {
		g.Matcher = m
	}
	arr, _ := obj["hooks"].([]any)
	for _, h := range arr {
		hObj, ok := h.(map[string]any)
		if !ok {
			continue
		}
		hh := handler{}
		if t, ok := hObj["type"].(string); ok {
			hh.Type = t
		}
		if c, ok := hObj["command"].(string); ok {
			hh.Command = c
		}
		g.Hooks = append(g.Hooks, hh)
	}
	return g, true
}
