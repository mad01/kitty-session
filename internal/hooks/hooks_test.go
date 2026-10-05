package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
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

func readGroups(t *testing.T, path string) map[string][]group {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	return groups(settings)
}

func countKS(gs []group) int {
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
	for _, event := range Events {
		if n := countKS(hooks[event]); n != 1 {
			t.Errorf("%s has %d ks groups after install, want 1", event, n)
		}
		last := hooks[event][len(hooks[event])-1]
		if last.Matcher != matchers[event] {
			t.Errorf("%s matcher = %q, want %q", event, last.Matcher, matchers[event])
		}
		if want := "~/bin/ks _hook"; last.Hooks[0].Command != want {
			t.Errorf("%s command = %q, want %q", event, last.Hooks[0].Command, want)
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
	for _, event := range Events {
		if n := countKS(hooks[event]); n != 0 {
			t.Errorf("%s has %d ks groups after uninstall, want 0", event, n)
		}
	}
	if _, ok := hooks["SessionEnd"]; ok {
		t.Error("SessionEnd key left behind after uninstall")
	}
	if len(hooks["Stop"]) != 1 || hooks["Stop"][0].Hooks[0].Command != "other-tool" {
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
	if hooks := readGroups(t, path); len(hooks) != 0 {
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
