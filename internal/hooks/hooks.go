// Package hooks builds the Claude Code hook registration ks relies on for
// state detection: the matcher groups that run `ks _hook` for the events in
// Events. Settings renders them as the inline settings document the launcher
// passes to every claude with --settings, so no file carries them. Older ks
// versions wrote the same groups into ~/.claude/settings.json; Leftovers
// finds those and Uninstall removes them, writing everything that is not a
// ks group back as read, fields ks does not model included.
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

// notifChannelDisabled is the preferredNotifChannel value that turns off
// Claude Code's own desktop notification.
const notifChannelDisabled = "notifications_disabled"

// event is one hook event ks registers, with the matcher its ks group
// carries.
type event struct {
	Name    string
	Matcher string
}

// events are the hook events ks registers, in reporting order: the one list
// Settings and Leftovers derive from. The Elicitation matcher is the MCP
// server name, so empty means every server.
var events = []event{
	{"UserPromptSubmit", ""},
	{"PreToolUse", ".*"},
	{"PostToolUse", ".*"},
	{"PostToolUseFailure", ".*"},
	{"PermissionRequest", ""},
	{"Elicitation", ""},
	{"ElicitationResult", ""},
	{"Stop", ""},
	{"StopFailure", ""},
	{"Notification", "permission_prompt|elicitation_dialog"},
	{"SessionStart", ""},
	{"SessionEnd", ReasonPromptInputExit + "|" + ReasonLogout},
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

// settings is the inline document Settings renders: the hooks object in the
// Claude Code settings format, plus the notification channel when muted.
type settings struct {
	Hooks                 map[string][]group `json:"hooks,omitempty"`
	PreferredNotifChannel string             `json:"preferredNotifChannel,omitempty"`
}

// Settings returns the settings document the launcher passes to claude as
// --settings: one ks group per event in events, each running the hook
// command for binary (hookCommand) with the handler timeout, and
// preferredNotifChannel set to notifications_disabled when quiet. An empty
// binary registers no hooks, so the document then carries only the quiet
// key. The output is one line with no trailing newline, with keys in sorted
// order, so two calls with the same input are byte-equal.
func Settings(binary string, quiet bool) string {
	var s settings
	if binary != "" {
		h := handler{Type: "command", Command: hookCommand(binary), Timeout: handlerTimeout}
		s.Hooks = make(map[string][]group, len(events))
		for _, ev := range events {
			s.Hooks[ev.Name] = []group{{Matcher: ev.Matcher, Hooks: []handler{h}}}
		}
	}
	if quiet {
		s.PreferredNotifChannel = notifChannelDisabled
	}
	data, err := json.Marshal(s)
	if err != nil {
		// Strings and ints only: a failure is a broken invariant, not input.
		panic("hooks: cannot marshal settings: " + err.Error())
	}
	return string(data)
}

// hookCommand is the command the injected groups run for binary. Claude
// Code hands it to sh -c, so a path with a space or a shell metacharacter
// goes in single quotes (shellQuote), and stays absolute, since ~ does not
// expand inside quotes. A plain path has $HOME shortened to ~, the form
// older ks versions wrote to the settings file: an identical leftover there
// is then the same command, which Claude Code runs once, instead of a
// second handler racing the injected one on the state file.
func hookCommand(binary string) string {
	if needsQuoting(binary) {
		return shellQuote(binary) + hookSuffix
	}
	return shortenHome(binary) + hookSuffix
}

// needsQuoting reports whether path has a character outside the set a
// shell takes as a plain word: letters, digits, _ . / and -.
func needsQuoting(path string) bool {
	return strings.ContainsFunc(path, func(r rune) bool {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return false
		case r == '_', r == '.', r == '/', r == '-':
			return false
		}
		return true
	})
}

// shellQuote wraps s in single quotes. An embedded quote closes the
// quotes, is escaped with a backslash, then reopens them, so the shell
// passes the whole string through as one word.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// shortenHome replaces the $HOME prefix of path with ~, at a path boundary
// only; a path not under $HOME, or with no $HOME to be had, is returned as
// is.
func shortenHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+"/") {
		return "~" + path[len(home):]
	}
	return path
}

// Path returns the Claude Code settings file, ~/.claude/settings.json.
func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

// Uninstall removes every ks group from the file: the entries an older ks
// wrote to the file with ks hooks install, whichever ks binary they name:
// with the hooks injected at launch, no ks group in the file is in use.
// Events left without groups are dropped, and so is an empty hooks key. An
// event whose value is not an array holds no ks group and is left alone.
func Uninstall(path string) error {
	settings, err := read(path)
	if err != nil {
		return err
	}
	hooks, err := hooksObject(settings, path)
	if err != nil {
		return err
	}
	for name, val := range hooks {
		gs, ok := val.([]any)
		if !ok {
			continue
		}
		if kept := without(gs); len(kept) > 0 {
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

// Leftovers returns the events that still have a ks group in the file, in
// reporting order: entries an older ks wrote, which the injected settings
// make redundant. Any command ending in " _hook" counts, whichever ks
// binary it names and however it is quoted. A missing settings file means
// none.
func Leftovers(path string) ([]string, error) {
	settings, err := read(path)
	if err != nil {
		return nil, err
	}
	hooks, err := hooksObject(settings, path)
	if err != nil {
		return nil, err
	}
	var left []string
	for _, ev := range events {
		gs, _ := hooks[ev.Name].([]any)
		if slices.ContainsFunc(gs, isKS) {
			left = append(left, ev.Name)
		}
	}
	return left, nil
}

// Status is how the ks hooks reach a session (injected at launch, always)
// and what an older ks left in the settings file: the file and the events
// with a leftover ks group, from one Leftovers read (Inspect). Full and
// Brief put it in words for the CLI and the sidebar.
type Status struct {
	Path      string
	Leftovers []string
}

// Inspect reads the settings file once and reports its leftovers.
func Inspect(path string) (Status, error) {
	left, err := Leftovers(path)
	if err != nil {
		return Status{}, err
	}
	return Status{Path: path, Leftovers: left}, nil
}

// injectedLine is the status with nothing left over, in both forms.
const injectedLine = "hooks injected at launch"

// Full is the CLI's status line: with leftovers it names the events, the
// file and the command that removes them.
func (s Status) Full() string {
	if len(s.Leftovers) == 0 {
		return injectedLine
	}
	return fmt.Sprintf(
		"%s; leftover global entries for %s in %s (ks hooks uninstall removes them)",
		injectedLine, strings.Join(s.Leftovers, ", "), s.Path,
	)
}

// Brief is the sidebar's status line, short enough for its one status row:
// with leftovers it gives their count and the command that removes them.
func (s Status) Brief() string {
	if len(s.Leftovers) == 0 {
		return injectedLine
	}
	return fmt.Sprintf("%s; %d leftover global entries (ks hooks uninstall)",
		injectedLine, len(s.Leftovers))
}

// isKS reports whether the raw group item runs a ks hook: any command ending
// in hookSuffix, whichever binary path (portable with ~ or absolute) an
// older ks wrote before it.
func isKS(item any) bool {
	return slices.ContainsFunc(commandsOf(item), func(c string) bool {
		return strings.HasSuffix(c, hookSuffix)
	})
}

// without returns gs minus the ks groups.
func without(gs []any) []any {
	var kept []any
	for _, g := range gs {
		if !isKS(g) {
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
