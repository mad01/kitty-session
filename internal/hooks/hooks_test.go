package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// seed has a foreign Stop hook and an unrelated key; both must survive every
// operation.
const seed = `{"theme":"dark","hooks":{"Stop":[{"matcher":"","hooks":[{"type":"command","command":"other-tool"}]}]}}`

func newSettings(t *testing.T, content string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".claude", "settings.json")
	if content == "" {
		return path
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// readHooks reads the settings file's hooks object as decoded JSON; nil
// without one.
func readHooks(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	hooks, _ := settings["hooks"].(map[string]any)
	return hooks
}

// readGroups is readHooks narrowed to the events whose value is an array,
// each as the raw groups the file holds.
func readGroups(t *testing.T, path string) map[string][]any {
	t.Helper()
	groups := map[string][]any{}
	for name, val := range readHooks(t, path) {
		if gs, ok := val.([]any); ok {
			groups[name] = gs
		}
	}
	return groups
}

// asGroup reads one raw group into the typed form for field assertions.
func asGroup(t *testing.T, item any) group {
	t.Helper()
	data, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	var g group
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

func countKS(gs []any) int {
	n := 0
	for _, g := range gs {
		if isKS(g) {
			n++
		}
	}
	return n
}

// ksGroups is the hooks object an older ks wrote for binary: one ks group
// per event, in the file's own shape.
func ksGroups(binary string) map[string]any {
	hooks := map[string]any{}
	for _, ev := range events {
		hooks[ev.Name] = []any{map[string]any{
			"matcher": ev.Matcher,
			"hooks": []any{map[string]any{
				"type": "command", "command": binary + " _hook", "timeout": float64(5),
			}},
		}}
	}
	return hooks
}

// settingsWith marshals a settings file holding hooks plus a theme key.
func settingsWith(t *testing.T, hooks map[string]any) string {
	t.Helper()
	data, err := json.Marshal(map[string]any{"theme": "dark", "hooks": hooks})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// wantMatchers is the matcher every event's injected group must carry.
var wantMatchers = map[string]string{
	"UserPromptSubmit":   "",
	"PreToolUse":         ".*",
	"PostToolUse":        ".*",
	"PostToolUseFailure": ".*",
	"PermissionRequest":  "",
	"Elicitation":        "",
	"ElicitationResult":  "",
	"Stop":               "",
	"StopFailure":        "",
	"Notification":       "permission_prompt|elicitation_dialog",
	"SessionStart":       "",
	"SessionEnd":         "prompt_input_exit|logout",
}

func TestSettings(t *testing.T) {
	t.Setenv("HOME", "/Users/me")
	const binary = "/Users/me/code/bin/ks" // under HOME: the command uses ~
	for _, quiet := range []bool{false, true} {
		t.Run(map[bool]string{false: "hooks only", true: "hooks and quiet"}[quiet], func(t *testing.T) {
			got := Settings(binary, quiet)
			if strings.ContainsAny(got, "\n\r") {
				t.Errorf("Settings is not one line: %q", got)
			}
			var doc struct {
				Hooks                 map[string][]group `json:"hooks"`
				PreferredNotifChannel *string            `json:"preferredNotifChannel"`
			}
			if err := json.Unmarshal([]byte(got), &doc); err != nil {
				t.Fatalf("Settings does not parse: %v\n%s", err, got)
			}
			if len(doc.Hooks) != len(wantMatchers) {
				t.Errorf("Settings has %d events, want %d: %v", len(doc.Hooks), len(wantMatchers), got)
			}
			for name, matcher := range wantMatchers {
				gs := doc.Hooks[name]
				if len(gs) != 1 {
					t.Errorf("%s has %d groups, want 1", name, len(gs))
					continue
				}
				if gs[0].Matcher != matcher {
					t.Errorf("%s matcher = %q, want %q", name, gs[0].Matcher, matcher)
				}
				if len(gs[0].Hooks) != 1 {
					t.Errorf("%s has %d handlers, want 1", name, len(gs[0].Hooks))
					continue
				}
				h := gs[0].Hooks[0]
				want := handler{Type: "command", Command: "~/code/bin/ks _hook", Timeout: 5}
				if h != want {
					t.Errorf("%s handler = %+v, want %+v", name, h, want)
				}
			}
			switch {
			case quiet && (doc.PreferredNotifChannel == nil ||
				*doc.PreferredNotifChannel != "notifications_disabled"):
				t.Errorf("quiet key missing or wrong: %v", doc.PreferredNotifChannel)
			case !quiet && doc.PreferredNotifChannel != nil:
				t.Errorf("quiet key present when not asked: %q", *doc.PreferredNotifChannel)
			}
			if again := Settings(binary, quiet); again != got {
				t.Errorf("Settings is not deterministic:\n%s\n%s", got, again)
			}
		})
	}
}

// Without a binary there are no hooks to register: the document carries the
// quiet key alone, or nothing at all.
func TestSettingsWithoutBinary(t *testing.T) {
	if got, want := Settings("", true), `{"preferredNotifChannel":"notifications_disabled"}`; got != want {
		t.Errorf("Settings(\"\", true) = %s, want %s", got, want)
	}
	if got, want := Settings("", false), `{}`; got != want {
		t.Errorf("Settings(\"\", false) = %s, want %s", got, want)
	}
}

// eventNames lists the registered events in reporting order.
func eventNames() []string {
	names := make([]string, len(events))
	for i, ev := range events {
		names[i] = ev.Name
	}
	return names
}

// The events a hook can fire, in reporting order; the handler keys off
// these names.
func TestEvents(t *testing.T) {
	want := []string{
		"UserPromptSubmit", "PreToolUse", "PostToolUse", "PostToolUseFailure",
		"PermissionRequest", "Elicitation", "ElicitationResult", "Stop", "StopFailure",
		"Notification", "SessionStart", "SessionEnd",
	}
	if got := eventNames(); !slices.Equal(got, want) {
		t.Errorf("events = %v, want %v", got, want)
	}
}

// TestHookCommand checks the command the injected groups run: ~ for a
// plain path under HOME, the path as is elsewhere, and single quotes
// around an absolute path with a space or a shell metacharacter, since ~
// would not expand inside them.
func TestHookCommand(t *testing.T) {
	t.Setenv("HOME", "/Users/me")
	tests := []struct {
		binary, want string
	}{
		{"/Users/me/code/bin/ks", "~/code/bin/ks _hook"},
		{"/Users/meow/ks", "/Users/meow/ks _hook"}, // a prefix, not a path boundary
		{"/opt/ks", "/opt/ks _hook"},
		{"/Users/me/My Tools/ks", "'/Users/me/My Tools/ks' _hook"},
		{"/Users/me/it's/ks", `'/Users/me/it'\''s/ks' _hook`},
		{"/Users/me/a&b/ks", "'/Users/me/a&b/ks' _hook"},
		{"/Users/me/$HOME/ks", "'/Users/me/$HOME/ks' _hook"},
	}
	for _, tt := range tests {
		if got := hookCommand(tt.binary); got != tt.want {
			t.Errorf("hookCommand(%q) = %q, want %q", tt.binary, got, tt.want)
		}
		if !strings.HasSuffix(hookCommand(tt.binary), hookSuffix) {
			t.Errorf("hookCommand(%q) does not end in %q", tt.binary, hookSuffix)
		}
	}
}

func TestUninstallRemovesLeftovers(t *testing.T) {
	hooks := ksGroups("~/bin/ks") // the portable form older ks versions wrote
	hooks["Stop"] = append(hooks["Stop"].([]any), map[string]any{
		"matcher": "", "hooks": []any{map[string]any{"type": "command", "command": "other-tool"}},
	})
	path := newSettings(t, settingsWith(t, hooks))

	left, err := Leftovers(path)
	if err != nil {
		t.Fatalf("Leftovers: %v", err)
	}
	if want := eventNames(); !slices.Equal(left, want) {
		t.Errorf("Leftovers = %v, want %v", left, want)
	}
	if err := Uninstall(path); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	got := readGroups(t, path)
	for _, ev := range events {
		if n := countKS(got[ev.Name]); n != 0 {
			t.Errorf("%s has %d ks groups after uninstall, want 0", ev.Name, n)
		}
	}
	if _, ok := got["SessionEnd"]; ok {
		t.Error("SessionEnd key left behind after uninstall")
	}
	if len(got["Stop"]) != 1 || asGroup(t, got["Stop"][0]).Hooks[0].Command != "other-tool" {
		t.Errorf("foreign Stop hook lost on uninstall: %+v", got["Stop"])
	}
	left, err = Leftovers(path)
	if err != nil {
		t.Fatalf("Leftovers: %v", err)
	}
	if left != nil {
		t.Errorf("Leftovers after uninstall = %v, want none", left)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	if settings["theme"] != "dark" {
		t.Errorf("theme = %v, want dark preserved", settings["theme"])
	}
}

// A group of another tool keeps every field across Uninstall, the ones ks
// does not model included: timeout, async, statusMessage and whatever a
// later Claude Code adds.
func TestForeignGroupsSurviveUnchanged(t *testing.T) {
	foreign := map[string]any{
		"matcher": "Bash",
		"hooks": []any{map[string]any{
			"type":          "command",
			"command":       "other-tool",
			"timeout":       float64(10),
			"async":         true,
			"statusMessage": "other-tool running",
		}},
	}
	path := newSettings(t, settingsWith(t, map[string]any{"PreToolUse": []any{foreign}}))
	if err := Uninstall(path); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	got := readGroups(t, path)["PreToolUse"]
	if len(got) == 0 || !reflect.DeepEqual(got[0], foreign) {
		t.Errorf("after Uninstall the foreign group is %v, want %v", got, foreign)
	}
}

// An event whose value is not an array keeps it whatever its shape: a
// hand-edited object under SubagentStop is not ks's to drop.
func TestNonArrayForeignEventSurvives(t *testing.T) {
	foreign := map[string]any{"bogus": true, "note": "hand edited"}
	path := newSettings(t, settingsWith(t, map[string]any{"SubagentStop": foreign}))
	if err := Uninstall(path); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if got := readHooks(t, path)["SubagentStop"]; !reflect.DeepEqual(got, foreign) {
		t.Errorf("after Uninstall SubagentStop is %v, want %v", got, foreign)
	}
}

// Every ks group goes, whichever binary wrote it: the portable form, the
// absolute form under this HOME, and a build at another path.
func TestUninstallRemovesEveryBinaryForm(t *testing.T) {
	path := newSettings(t, "")
	absolute := filepath.Join(os.Getenv("HOME"), "bin", "ks")
	var groups []any
	for _, binary := range []string{"~/bin/ks", absolute, "/opt/old/ks"} {
		groups = append(groups, map[string]any{
			"matcher": "",
			"hooks":   []any{map[string]any{"type": "command", "command": binary + " _hook"}},
		})
	}
	content, err := json.Marshal(map[string]any{"hooks": map[string]any{"Stop": groups}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(path); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if hooks := readHooks(t, path); len(hooks) != 0 {
		t.Errorf("hooks left after uninstall: %+v", hooks)
	}
}

func TestLeftovers(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{"missing file", "", nil},
		{"no hooks key", `{"theme":"dark"}`, nil},
		{
			"three events from other ks binaries, one quoted",
			`{"hooks":{"Stop":[{"matcher":"","hooks":[{"type":"command","command":"/opt/ks _hook"}]}],` +
				`"PreToolUse":[{"matcher":".*","hooks":[{"type":"command","command":"~/bin/ks _hook"}]}],` +
				`"SessionEnd":[{"matcher":"","hooks":[{"type":"command","command":"'/opt/my ks/ks' _hook"}]}],` +
				`"Notification":[{"matcher":"","hooks":[{"type":"command","command":"other-tool"}]}]}}`,
			[]string{"PreToolUse", "Stop", "SessionEnd"},
		},
		{
			"a ks event that is not an array holds no ks group",
			`{"hooks":{"Stop":{"hooks":[{"type":"command","command":"~/bin/ks _hook"}]}}}`,
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := newSettings(t, tt.content)
			got, err := Leftovers(path)
			if err != nil {
				t.Fatalf("Leftovers: %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Leftovers = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLeftoversRejectsBadJSON(t *testing.T) {
	path := newSettings(t, `{not json`)
	if _, err := Leftovers(path); err == nil {
		t.Fatal("Leftovers accepted malformed settings")
	}
}

func TestStatus(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantFull  string // with PATH standing for the settings file
		wantBrief string
	}{
		{"missing file", "", "hooks injected at launch", "hooks injected at launch"},
		{"only foreign hooks", seed, "hooks injected at launch", "hooks injected at launch"},
		{
			"leftovers",
			`{"hooks":{"Stop":[{"matcher":"","hooks":[{"type":"command","command":"~/bin/ks _hook"}]}],` +
				`"PreToolUse":[{"matcher":".*","hooks":[{"type":"command","command":"~/bin/ks _hook"}]}]}}`,
			"hooks injected at launch; leftover global entries for PreToolUse, Stop in PATH " +
				"(ks hooks uninstall removes them)",
			"hooks injected at launch; 2 leftover global entries (ks hooks uninstall)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := newSettings(t, tt.content)
			st, err := Inspect(path)
			if err != nil {
				t.Fatalf("Inspect: %v", err)
			}
			if st.Path != path {
				t.Errorf("Path = %q, want %q", st.Path, path)
			}
			if want := strings.ReplaceAll(tt.wantFull, "PATH", path); st.Full() != want {
				t.Errorf("Full = %q, want %q", st.Full(), want)
			}
			if st.Brief() != tt.wantBrief {
				t.Errorf("Brief = %q, want %q", st.Brief(), tt.wantBrief)
			}
		})
	}
	path := newSettings(t, `{not json`)
	if _, err := Inspect(path); err == nil {
		t.Fatal("Inspect accepted malformed settings")
	}
}
