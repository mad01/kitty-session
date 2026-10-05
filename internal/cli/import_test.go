package cli

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mad01/kitty-session/internal/claude"
	"github.com/mad01/kitty-session/internal/session"
)

const (
	alphaID = "b75ee90c-9382-4d0a-a09b-99b7fd5f34ef"
	betaID  = "211b8c21-c5a0-4fa1-864a-8d6c8d72f67c"
)

// herdrFixture is a version 3 herdr session file with two claude agents
// (alpha unnamed, beta in a workspace the user named), a shell pane and a
// codex pane, all rooted under dirs.
func herdrFixture(alphaDir, betaDir string) string {
	return fmt.Sprintf(`{
  "version": 3,
  "workspaces": [
    {"id": "w1", "custom_name": null, "identity_cwd": %[1]q, "tabs": [
      {"custom_name": null, "layout": {"Pane": 1}, "panes": {
        "1": {"cwd": %[1]q, "agent_session":
          {"source": "herdr:claude", "agent": "claude", "kind": "id", "value": %[3]q}}
      }, "zoomed": false, "focused": 1, "root_pane": 1}
    ], "active_tab": 0},
    {"id": "w2", "custom_name": "Beta Work", "identity_cwd": %[2]q, "tabs": [
      {"custom_name": null, "layout": {"Pane": 2}, "panes": {
        "2": {"cwd": %[2]q, "agent_session":
          {"source": "herdr:claude", "agent": "claude", "kind": "id", "value": %[4]q}},
        "3": {"cwd": %[2]q},
        "4": {"cwd": %[2]q, "agent_session":
          {"source": "herdr:codex", "agent": "codex", "kind": "id", "value": "thread-1"}}
      }, "zoomed": false, "focused": 2, "root_pane": 2}
    ], "active_tab": 0}
  ],
  "active": 0, "selected": 0
}`, alphaDir, betaDir, alphaID, betaID)
}

// importFixture sets a fake HOME, writes a herdr fixture into dir (a short
// temp dir, so a unix socket fits beside it) and returns HOME, the fixture
// path and the two agent directories, both under HOME.
func importFixture(t *testing.T) (home, fixture, alphaDir, betaDir string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	alphaDir = filepath.Join(home, "code", "alpha")
	betaDir = filepath.Join(home, "code", "beta")
	dir, err := os.MkdirTemp("", "ks")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	fixture = filepath.Join(dir, "session.json")
	if err := os.WriteFile(fixture, []byte(herdrFixture(alphaDir, betaDir)), 0o644); err != nil {
		t.Fatal(err)
	}
	return home, fixture, alphaDir, betaDir
}

// writeTranscript creates the transcript file Claude Code would keep for
// sessionID started in dir, under the fake HOME.
func writeTranscript(t *testing.T, dir, sessionID string) string {
	t.Helper()
	path, err := claude.TranscriptPath(dir, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// runImportCmd runs ks import with args and returns stdout, stderr and the
// error, with the command's flags reset on both sides.
func runImportCmd(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errOut)
	rootCmd.SetArgs(append([]string{"import"}, args...))
	resetImportFlags()
	err = rootCmd.Execute()
	rootCmd.SetArgs(nil)
	rootCmd.SetOut(nil)
	rootCmd.SetErr(nil)
	resetImportFlags()
	return out.String(), errOut.String(), err
}

func resetImportFlags() {
	importDryRun = false
	importFrom = ""
	importNoOpen = false
}

// row returns the whitespace-split fields of the output line whose first
// two fields are label and name, or nil.
func row(output, label, name string) []string {
	for _, line := range strings.Split(output, "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == label && f[1] == name {
			return f
		}
	}
	return nil
}

func sessionFiles(t *testing.T, home string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(home, ".config", "ks", "sessions", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

func TestImportDryRunWritesNothing(t *testing.T) {
	home, fixture, _, _ := importFixture(t)

	out, _, err := runImportCmd(t, "--dry-run", "--from", fixture, "--no-open")
	if err != nil {
		t.Fatalf("execute: %v\n%s", err, out)
	}
	if got := row(out, "import", "alpha"); len(got) != 4 || got[3] != alphaID[:8] {
		t.Errorf("alpha row = %v", got)
	}
	if !strings.Contains(out, "ks: dry run, nothing written: 2 to import, 0 already present, 2 skipped") {
		t.Errorf("summary missing in:\n%s", out)
	}
	if files := sessionFiles(t, home); len(files) != 0 {
		t.Errorf("dry run wrote %v", files)
	}
}

func TestImportWritesActiveRecords(t *testing.T) {
	home, fixture, alphaDir, betaDir := importFixture(t)
	alphaTranscript := writeTranscript(t, alphaDir, alphaID)

	out, errOut, err := runImportCmd(t, "--from", fixture, "--no-open")
	if err != nil {
		t.Fatalf("execute: %v\n%s%s", err, out, errOut)
	}

	alphaRow := row(out, "imported", "alpha")
	if len(alphaRow) != 4 || alphaRow[2] != "~/code/alpha" || alphaRow[3] != alphaID[:8] {
		t.Errorf("alpha row = %v", alphaRow)
	}
	if got := row(out, "imported", "beta-work"); len(got) != 4 || got[3] != betaID[:8] {
		t.Errorf("beta row = %v", got)
	}
	if got := row(out, "skipped", "~/code/beta"); !strings.HasSuffix(strings.Join(got, " "), "shell, no agent") {
		t.Errorf("shell skip row = %v", got)
	}
	if !strings.Contains(out, "codex is not claude") {
		t.Errorf("codex skip missing in:\n%s", out)
	}
	if !strings.Contains(out, "ks: 2 imported, 0 already present, 2 skipped") {
		t.Errorf("summary missing in:\n%s", out)
	}
	if !strings.Contains(errOut, "warning: beta-work: transcript missing, will start fresh") {
		t.Errorf("beta warning missing in stderr:\n%s", errOut)
	}
	if strings.Contains(errOut, "alpha") {
		t.Errorf("alpha has a transcript and should not warn:\n%s", errOut)
	}

	store, err := session.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	alpha, err := store.Load("alpha")
	if err != nil {
		t.Fatalf("alpha record: %v", err)
	}
	if alpha.ID == "" || alpha.Dir != alphaDir || alpha.ClaudeSessionID != alphaID ||
		alpha.ClaudeTranscriptPath != alphaTranscript || !alpha.IsActive() ||
		alpha.Status != session.StatusActive {
		t.Errorf("alpha record = %+v", alpha)
	}
	if alpha.KittyTabID != 0 || alpha.KittyWindowID != 0 || alpha.KittySidebarWindowID != 0 {
		t.Errorf("imported record must own no kitty window: %+v", alpha)
	}
	beta, err := store.Load("beta-work")
	if err != nil {
		t.Fatalf("beta record: %v", err)
	}
	if beta.Dir != betaDir || beta.ClaudeSessionID != betaID || beta.ClaudeTranscriptPath == "" {
		t.Errorf("beta record = %+v", beta)
	}
	if files := sessionFiles(t, home); len(files) != 2 {
		t.Errorf("expected 2 session files, got %v", files)
	}
}

func TestImportSecondRunReportsExisting(t *testing.T) {
	_, fixture, _, _ := importFixture(t)
	if _, _, err := runImportCmd(t, "--from", fixture, "--no-open"); err != nil {
		t.Fatal(err)
	}

	out, _, err := runImportCmd(t, "--from", fixture, "--no-open")
	if err != nil {
		t.Fatalf("second run: %v\n%s", err, out)
	}
	for _, name := range []string{"alpha", "beta-work"} {
		if got := row(out, "exists", name); len(got) != 4 {
			t.Errorf("%s exists row = %v in:\n%s", name, got, out)
		}
	}
	if !strings.Contains(out, "ks: 0 imported, 2 already present, 2 skipped") {
		t.Errorf("summary missing in:\n%s", out)
	}
}

func TestImportSuffixesTakenNames(t *testing.T) {
	_, fixture, alphaDir, _ := importFixture(t)
	store, err := session.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alpha", "alpha-2"} {
		other := session.New(name, alphaDir, 0, 0)
		other.ClaudeSessionID = "another-" + name
		if err := store.Save(other); err != nil {
			t.Fatal(err)
		}
	}

	out, _, err := runImportCmd(t, "--from", fixture, "--no-open")
	if err != nil {
		t.Fatalf("execute: %v\n%s", err, out)
	}
	if got := row(out, "imported", "alpha-3"); len(got) != 4 || got[3] != alphaID[:8] {
		t.Errorf("expected alpha-3 row, got %v in:\n%s", got, out)
	}
	sess, err := store.Load("alpha-3")
	if err != nil {
		t.Fatalf("alpha-3 record: %v", err)
	}
	if sess.ClaudeSessionID != alphaID {
		t.Errorf("alpha-3 carries %q", sess.ClaudeSessionID)
	}
}

func TestImportMissingFileNamesPathAndFlag(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	missing := filepath.Join(t.TempDir(), "session.json")

	_, _, err := runImportCmd(t, "--from", missing, "--no-open")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), missing) || !strings.Contains(err.Error(), "--from") {
		t.Errorf("error should name the path and --from: %v", err)
	}
}

func TestImportNoAgentsIsNotAnError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	fixture := filepath.Join(t.TempDir(), "session.json")
	if err := os.WriteFile(fixture, []byte(`{"version": 3, "workspaces": []}`), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _, err := runImportCmd(t, "--from", fixture)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "no agents found") {
		t.Errorf("output = %q", out)
	}
	if files := sessionFiles(t, home); len(files) != 0 {
		t.Errorf("wrote %v", files)
	}
}

func TestImportWarnsWhileHerdrRuns(t *testing.T) {
	_, fixture, _, _ := importFixture(t)
	l, err := net.Listen("unix", filepath.Join(filepath.Dir(fixture), "herdr.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })

	out, errOut, err := runImportCmd(t, "--from", fixture, "--no-open")
	if err != nil {
		t.Fatalf("execute: %v\n%s", err, out)
	}
	if !strings.Contains(errOut, "herdr is still running") ||
		!strings.Contains(errOut, "herdr session stop default") {
		t.Errorf("expected the herdr-running note on stderr, got:\n%s", errOut)
	}
	if !strings.Contains(out, "ks: 2 imported") {
		t.Errorf("records should still be written:\n%s", out)
	}
}
