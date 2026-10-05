package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// ksEvents are the Claude Code events ks hooks install must register.
var ksEvents = []string{"PreToolUse", "Stop", "Notification", "SessionStart", "SessionEnd"}

func TestHooksInstallAndUninstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// A foreign Stop hook and an unrelated key must survive both commands.
	seed := `{"theme":"dark","hooks":{"Stop":[{"matcher":"","hooks":[{"type":"command","command":"other-tool"}]}]}}`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	// Install twice: the second run must not duplicate any ks group.
	runHooksCmd(t, "install")
	runHooksCmd(t, "install")
	hooks := readHooks(t, path)
	for _, event := range ksEvents {
		if n := countKsGroups(hooks[event]); n != 1 {
			t.Errorf("%s has %d ks groups after install, want 1", event, n)
		}
	}
	if countForeign(hooks["Stop"]) != 1 {
		t.Error("foreign Stop hook lost on install")
	}

	runHooksCmd(t, "uninstall")
	hooks = readHooks(t, path)
	for _, event := range ksEvents {
		if n := countKsGroups(hooks[event]); n != 0 {
			t.Errorf("%s has %d ks groups after uninstall, want 0", event, n)
		}
	}
	if _, ok := hooks["SessionEnd"]; ok {
		t.Error("SessionEnd key left behind after uninstall")
	}
	if countForeign(hooks["Stop"]) != 1 {
		t.Error("foreign Stop hook lost on uninstall")
	}

	var settings map[string]any
	if err := json.Unmarshal(readFile(t, path), &settings); err != nil {
		t.Fatal(err)
	}
	if settings["theme"] != "dark" {
		t.Errorf("theme = %v, want dark preserved", settings["theme"])
	}
}

func runHooksCmd(t *testing.T, sub string) {
	t.Helper()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"hooks", sub})
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("hooks %s: %v (output: %s)", sub, err, buf.String())
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func readHooks(t *testing.T, path string) map[string][]matcherGroup {
	t.Helper()
	var settings map[string]any
	if err := json.Unmarshal(readFile(t, path), &settings); err != nil {
		t.Fatal(err)
	}
	return getOrCreateHooksMap(settings)
}

func isKsGroup(g matcherGroup) bool {
	return slices.ContainsFunc(g.Hooks, func(h hookHandler) bool {
		return strings.HasSuffix(h.Command, " _hook")
	})
}

func countKsGroups(groups []matcherGroup) int {
	n := 0
	for _, g := range groups {
		if isKsGroup(g) {
			n++
		}
	}
	return n
}

func countForeign(groups []matcherGroup) int {
	return len(groups) - countKsGroups(groups)
}
