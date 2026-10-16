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
		if isKS(g, nil) {
			n++
		}
	}
	return n
}

func TestInstallAndUninstall(t *testing.T) {
	path := newSettings(t, seed)
	binary := filepath.Join(os.Getenv("HOME"), "bin", "ks")

	// Install twice: the second run must not duplicate any ks group.
	for range 2 {
		if err := Install(path, binary); err != nil {
			t.Fatalf("Install: %v", err)
		}
	}
	hooks := readGroups(t, path)
	for _, ev := range events {
		gs := hooks[ev.Name]
		if n := countKS(gs); n != 1 {
			t.Errorf("%s has %d ks groups after install, want 1", ev.Name, n)
		}
		last := asGroup(t, gs[len(gs)-1])
		if last.Matcher != ev.Matcher {
			t.Errorf("%s matcher = %q, want %q", ev.Name, last.Matcher, ev.Matcher)
		}
		if want := "~/bin/ks _hook"; last.Hooks[0].Command != want {
			t.Errorf("%s command = %q, want %q", ev.Name, last.Hooks[0].Command, want)
		}
		if last.Hooks[0].Timeout != handlerTimeout {
			t.Errorf("%s timeout = %d, want %d", ev.Name, last.Hooks[0].Timeout, handlerTimeout)
		}
	}
	if len(hooks["Stop"])-countKS(hooks["Stop"]) != 1 {
		t.Error("foreign Stop hook lost on install")
	}
	installed, err := Installed(path)
	if err != nil {
		t.Fatalf("Installed: %v", err)
	}
	if !slices.Equal(installed, Events) {
		t.Errorf("Installed = %v, want %v", installed, Events)
	}

	if err := Uninstall(path, binary); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	hooks = readGroups(t, path)
	for _, ev := range events {
		if n := countKS(hooks[ev.Name]); n != 0 {
			t.Errorf("%s has %d ks groups after uninstall, want 0", ev.Name, n)
		}
	}
	if _, ok := hooks["SessionEnd"]; ok {
		t.Error("SessionEnd key left behind after uninstall")
	}
	if len(hooks["Stop"]) != 1 || asGroup(t, hooks["Stop"][0]).Hooks[0].Command != "other-tool" {
		t.Errorf("foreign Stop hook lost on uninstall: %+v", hooks["Stop"])
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

// A group of another tool keeps every field across Install and Uninstall,
// the ones ks does not model included: timeout, async, statusMessage and
// whatever a later Claude Code adds.
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
	content, err := json.Marshal(map[string]any{
		"hooks": map[string]any{"PreToolUse": []any{foreign}},
	})
	if err != nil {
		t.Fatal(err)
	}
	path := newSettings(t, string(content))
	binary := filepath.Join(os.Getenv("HOME"), "bin", "ks")
	steps := []struct {
		name string
		run  func(path, binary string) error
	}{{"Install", Install}, {"Uninstall", Uninstall}}
	for _, step := range steps {
		if err := step.run(path, binary); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		got := readGroups(t, path)["PreToolUse"]
		if len(got) == 0 || !reflect.DeepEqual(got[0], foreign) {
			t.Errorf("after %s the foreign group is %v, want %v", step.name, got, foreign)
		}
	}
}

// An event ks does not register keeps its value whatever its shape: a
// hand-edited object under SubagentStop is not ks's to drop.
func TestNonArrayForeignEventSurvives(t *testing.T) {
	foreign := map[string]any{"bogus": true, "note": "hand edited"}
	content, err := json.Marshal(map[string]any{
		"hooks": map[string]any{"SubagentStop": foreign},
	})
	if err != nil {
		t.Fatal(err)
	}
	path := newSettings(t, string(content))
	binary := filepath.Join(os.Getenv("HOME"), "bin", "ks")
	steps := []struct {
		name string
		run  func(path, binary string) error
	}{{"Install", Install}, {"Uninstall", Uninstall}}
	for _, step := range steps {
		if err := step.run(path, binary); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		if got := readHooks(t, path)["SubagentStop"]; !reflect.DeepEqual(got, foreign) {
			t.Errorf("after %s SubagentStop is %v, want %v", step.name, got, foreign)
		}
	}
}

// A ks event whose value is not an array cannot take a ks group without
// being dropped, so Install refuses, names it, and leaves the file alone.
func TestInstallRejectsNonArrayKSEvent(t *testing.T) {
	content := `{"hooks":{"Stop":{"matcher":"","hooks":[]}}}`
	path := newSettings(t, content)
	binary := filepath.Join(os.Getenv("HOME"), "bin", "ks")
	err := Install(path, binary)
	if err == nil {
		t.Fatal("Install accepted a Stop object")
	}
	want := "hooks.Stop in " + path + " is not an array"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("Install error = %q, want it to contain %q", err, want)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != content {
		t.Errorf("settings rewritten on a refused install: %s", data)
	}
}

func TestUninstallRemovesTheAbsoluteForm(t *testing.T) {
	path := newSettings(t, "")
	binary := filepath.Join(os.Getenv("HOME"), "bin", "ks")
	old := `{"hooks":{"Stop":[{"matcher":"","hooks":[{"type":"command","command":"` +
		binary + ` _hook"}]}]}}`
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(path, binary); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if hooks := readHooks(t, path); len(hooks) != 0 {
		t.Errorf("hooks left after uninstall: %+v", hooks)
	}
}

func TestInstalled(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{"missing file", "", nil},
		{"no hooks key", `{"theme":"dark"}`, nil},
		{
			"two events from another ks binary",
			`{"hooks":{"Stop":[{"matcher":"","hooks":[{"type":"command","command":"/opt/ks _hook"}]}],` +
				`"PreToolUse":[{"matcher":".*","hooks":[{"type":"command","command":"~/bin/ks _hook"}]}],` +
				`"Notification":[{"matcher":"","hooks":[{"type":"command","command":"other-tool"}]}]}}`,
			[]string{"PreToolUse", "Stop"},
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
			got, err := Installed(path)
			if err != nil {
				t.Fatalf("Installed: %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Installed = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInstalledRejectsBadJSON(t *testing.T) {
	path := newSettings(t, `{not json`)
	if _, err := Installed(path); err == nil {
		t.Fatal("Installed accepted malformed settings")
	}
}
