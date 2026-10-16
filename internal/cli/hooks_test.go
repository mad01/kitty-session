package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ksSettings is a settings file holding one ks group an older ks wrote with
// binary under Stop and PreToolUse, beside a foreign PreToolUse group.
func ksSettings(binary string) string {
	ks := `{"matcher":"","hooks":[{"type":"command","command":"` + binary + ` _hook","timeout":5}]}`
	return `{"theme":"dark","hooks":{"Stop":[` + ks + `],` +
		`"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"other-tool"}]},` + ks + `]}}`
}

// foreignSettings is a settings file with no ks group, written the way a
// settings manager would (no indentation, keys in its own order).
const foreignSettings = `{"theme":"dark","hooks":{"Stop":[{"matcher":"","hooks":[{"type":"command","command":"other-tool"}]}]}}`

// seedClaudeSettings writes content as ~/.claude/settings.json under a fake
// HOME and returns the file's path.
func seedClaudeSettings(t *testing.T, content string) string {
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

// runHooksCmd runs ks hooks <args> and returns what it printed.
func runHooksCmd(t *testing.T, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs(append([]string{"hooks"}, args...))
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("ks hooks %s: %v\n%s", strings.Join(args, " "), err, out.String())
	}
	return out.String()
}

// readSettingsHooks returns the hooks object of the settings file, nil
// without one.
func readSettingsHooks(t *testing.T, path string) map[string]any {
	t.Helper()
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
	hooks, _ := settings["hooks"].(map[string]any)
	return hooks
}

// readFile returns the settings file's bytes as they are on disk.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// Leftovers go whichever ks binary wrote them; here a build at a path this
// test binary never had.
func TestHooksInstallRemovesLeftovers(t *testing.T) {
	path := seedClaudeSettings(t, ksSettings("/opt/old/ks"))

	out := runHooksCmd(t, "install")
	want := "ks hooks are injected at launch; nothing to install\n" +
		"removed leftover ks hooks from " + path + "\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	hooks := readSettingsHooks(t, path)
	if _, ok := hooks["Stop"]; ok {
		t.Errorf("Stop left behind: %v", hooks["Stop"])
	}
	pre, _ := hooks["PreToolUse"].([]any)
	if len(pre) != 1 {
		t.Fatalf("PreToolUse = %v, want the foreign group alone", hooks["PreToolUse"])
	}
	if got := runHooksCmd(t, "status"); got != "hooks injected at launch\n" {
		t.Errorf("status after install = %q", got)
	}
}

// Without leftovers neither subcommand touches the file: a settings manager
// that regenerates it must not see a rewrite.
func TestHooksLeaveAFileWithoutLeftoversAlone(t *testing.T) {
	path := seedClaudeSettings(t, foreignSettings)

	out := runHooksCmd(t, "install")
	if want := "ks hooks are injected at launch; nothing to install\n"; out != want {
		t.Errorf("install output = %q, want %q", out, want)
	}
	if got := readFile(t, path); got != foreignSettings {
		t.Errorf("install rewrote the file:\n%s", got)
	}

	out = runHooksCmd(t, "uninstall")
	if want := "no leftover ks hooks in " + path + "\n"; out != want {
		t.Errorf("uninstall output = %q, want %q", out, want)
	}
	if got := readFile(t, path); got != foreignSettings {
		t.Errorf("uninstall rewrote the file:\n%s", got)
	}

	missing := seedClaudeSettings(t, "")
	if out := runHooksCmd(t, "install"); !strings.HasPrefix(out, "ks hooks are injected") {
		t.Errorf("install output without a settings file = %q", out)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Errorf("install created a settings file: %v", err)
	}
}

func TestHooksStatus(t *testing.T) {
	path := seedClaudeSettings(t, ksSettings("/opt/old/ks"))
	out := runHooksCmd(t, "status")
	want := "hooks injected at launch; leftover global entries for PreToolUse, Stop in " +
		path + " (ks hooks uninstall removes them)\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}

	seedClaudeSettings(t, "")
	if out := runHooksCmd(t, "status"); out != "hooks injected at launch\n" {
		t.Errorf("output without a settings file = %q", out)
	}
}

func TestHooksUninstall(t *testing.T) {
	path := seedClaudeSettings(t, ksSettings("~/code/bin/ks")) // the portable form
	out := runHooksCmd(t, "uninstall")
	if want := "removed leftover ks hooks from " + path + "\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	hooks := readSettingsHooks(t, path)
	if len(hooks) != 1 {
		t.Errorf("hooks after uninstall = %v, want the foreign PreToolUse group alone", hooks)
	}
}
