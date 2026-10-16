// Package hooks edits the Claude Code hook registration ks relies on for state
// detection: the matcher groups in ~/.claude/settings.json that run `ks _hook`
// for the events in Events. Install and Uninstall rewrite the file and write
// everything that is not a ks group back as they read it, fields ks does not
// model included; Installed reads it.
package hooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
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

// handlerTimeout is the seconds Claude Code gives a ks handler before it
// kills it. The handler only writes a state file, and PermissionRequest sits
// synchronously in Claude's permission flow, where the default of 600 s
// would hold the dialog for a hung handler.
const handlerTimeout = 5

// event is one hook event ks registers, with the matcher its ks group
// carries.
type event struct {
	Name    string
	Matcher string
}

// events are the hook events ks registers, in reporting order: the one list
// Install, Installed and Events derive from.
var events = []event{
	{"UserPromptSubmit", ""},
	{"PreToolUse", ".*"},
	{"PostToolUse", ".*"},
	{"PostToolUseFailure", ".*"},
	{"PermissionRequest", ""},
	{"Stop", ""},
	{"StopFailure", ""},
	{"Notification", "permission_prompt|elicitation_dialog"},
	{"SessionStart", ""},
	{"SessionEnd", ReasonPromptInputExit + "|" + ReasonLogout},
}

// Events are the names of the hook events ks registers, in reporting order.
var Events = eventNames()

func eventNames() []string {
	names := make([]string, len(events))
	for i, ev := range events {
		names[i] = ev.Name
	}
	return names
}

// handler is one hook command in the Claude Code settings format.
type handler struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

// group is a matcher with the handlers it runs: the shape ks writes. Groups
// read from the file stay in their raw JSON form so that rewriting the file
// keeps whatever fields other tools set.
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
// settings back. Only the ks events' arrays change; everything else in the
// hooks object is kept as it was. A ks event whose value is not an array is
// an error, since ks cannot add to it without dropping it.
func Install(path, binary string) error {
	settings, err := read(path)
	if err != nil {
		return err
	}
	hooks, err := hooksObject(settings, path)
	if err != nil {
		return err
	}
	cmds := commands(binary)
	ks := handler{Type: "command", Command: cmds[0], Timeout: handlerTimeout}
	for _, ev := range events {
		gs, err := eventGroups(hooks, ev.Name, path)
		if err != nil {
			return err
		}
		hooks[ev.Name] = append(without(gs, cmds), group{Matcher: ev.Matcher, Hooks: []handler{ks}})
	}
	settings["hooks"] = hooks
	return write(path, settings)
}

// Uninstall removes every ks group for binary (portable or absolute form).
// Events left without groups are dropped, and so is an empty hooks key. An
// event whose value is not an array holds no ks group and is left alone.
func Uninstall(path, binary string) error {
	settings, err := read(path)
	if err != nil {
		return err
	}
	hooks, err := hooksObject(settings, path)
	if err != nil {
		return err
	}
	cmds := commands(binary)
	for name, val := range hooks {
		gs, ok := val.([]any)
		if !ok {
			continue
		}
		if kept := without(gs, cmds); len(kept) > 0 {
			hooks[name] = kept
		} else {
			delete(hooks, name)
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
	hooks, err := hooksObject(settings, path)
	if err != nil {
		return nil, err
	}
	var installed []string
	for _, ev := range events {
		gs, _ := hooks[ev.Name].([]any)
		if slices.ContainsFunc(gs, func(item any) bool { return isKS(item, nil) }) {
			installed = append(installed, ev.Name)
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

// isKS reports whether the raw group item runs one of cmds, or with nil cmds
// any ks hook.
func isKS(item any, cmds []string) bool {
	for _, c := range commandsOf(item) {
		if cmds == nil && strings.HasSuffix(c, hookSuffix) {
			return true
		}
		if slices.Contains(cmds, c) {
			return true
		}
	}
	return false
}

// without returns gs minus the groups that run one of cmds.
func without(gs []any, cmds []string) []any {
	var kept []any
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

// hooksObject returns the hooks object of settings itself, so that whatever
// it holds beyond the ks events goes back to the file as read; an empty one
// when the key is absent. Any other value is an error.
func hooksObject(settings map[string]any, path string) (map[string]any, error) {
	raw, ok := settings["hooks"]
	if !ok || raw == nil {
		return map[string]any{}, nil
	}
	hooks, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("hooks in %s is not an object", path)
	}
	return hooks, nil
}

// eventGroups returns the groups registered for event in their raw form,
// none when the key is absent, and an error when its value is not an array.
func eventGroups(hooks map[string]any, event, path string) ([]any, error) {
	raw, ok := hooks[event]
	if !ok || raw == nil {
		return nil, nil
	}
	gs, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("hooks.%s in %s is not an array", event, path)
	}
	return gs, nil
}

// commandsOf returns the commands the raw group item runs. The command is
// the only field ks reads from a group it did not write; a malformed item
// has none, so it is never a ks group and goes back to the file untouched.
func commandsOf(item any) []string {
	obj, ok := item.(map[string]any)
	if !ok {
		return nil
	}
	arr, _ := obj["hooks"].([]any)
	var cmds []string
	for _, h := range arr {
		hObj, ok := h.(map[string]any)
		if !ok {
			continue
		}
		if c, ok := hObj["command"].(string); ok {
			cmds = append(cmds, c)
		}
	}
	return cmds
}
