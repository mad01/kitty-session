package cli

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mad01/kitty-session/internal/claude"
	"github.com/mad01/kitty-session/internal/herdr"
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
	importTo = targetKs
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
	if !strings.Contains(
		out,
		"ks: dry run, nothing written: 2 to import, 0 to update, 0 already present, 2 skipped",
	) {
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
	if got := row(out, "skipped", "~/code/beta"); !strings.HasSuffix(
		strings.Join(got, " "),
		"shell, no agent",
	) {
		t.Errorf("shell skip row = %v", got)
	}
	if !strings.Contains(out, "codex is not claude") {
		t.Errorf("codex skip missing in:\n%s", out)
	}
	if !strings.Contains(out, "ks: 2 imported, 0 updated, 0 already present, 2 skipped") {
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
	if !strings.Contains(out, "ks: 0 imported, 0 updated, 2 already present, 2 skipped") {
		t.Errorf("summary missing in:\n%s", out)
	}
}

// The taken names belong to records in another directory, so they are name
// collisions only and no directory match updates one of them.
func TestImportSuffixesTakenNames(t *testing.T) {
	home, fixture, _, _ := importFixture(t)
	otherDir := filepath.Join(home, "code", "elsewhere")
	store, err := session.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alpha", "alpha-2"} {
		other := session.New(name, otherDir, 0, 0)
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

// saveRecord stores an active record for dir carrying claudeID, as a
// previous import or a ks new with hooks would have left it.
func saveRecord(t *testing.T, store *session.Store, name, dir, claudeID string) *session.Session {
	t.Helper()
	sess := session.New(name, dir, 0, 0)
	sess.ClaudeSessionID = claudeID
	sess.ClaudeTranscriptPath = "/old/" + claudeID + ".jsonl"
	if err := store.Save(sess); err != nil {
		t.Fatal(err)
	}
	return sess
}

func TestImportUpdatesRecordForDirectory(t *testing.T) {
	home, fixture, alphaDir, _ := importFixture(t)
	transcript := writeTranscript(t, alphaDir, alphaID)
	store, err := session.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	old := saveRecord(t, store, "alpha", alphaDir, "stale-alpha")

	out, errOut, err := runImportCmd(t, "--from", fixture, "--no-open")
	if err != nil {
		t.Fatalf("execute: %v\n%s%s", err, out, errOut)
	}
	if got := row(out, "updated", "alpha"); len(got) != 4 || got[3] != alphaID[:8] {
		t.Errorf("alpha row = %v in:\n%s", got, out)
	}
	if !strings.Contains(out, "ks: 1 imported, 1 updated, 0 already present, 2 skipped") {
		t.Errorf("summary missing in:\n%s", out)
	}
	if strings.Contains(errOut, "alpha") {
		t.Errorf("alpha has a transcript and should not warn:\n%s", errOut)
	}
	got, err := store.Load("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != old.ID || got.ClaudeSessionID != alphaID ||
		got.ClaudeTranscriptPath != transcript || !got.IsActive() {
		t.Errorf("alpha record = %+v", got)
	}
	if files := sessionFiles(t, home); len(files) != 2 {
		t.Errorf("expected alpha and beta-work only, got %v", files)
	}
}

func TestImportDryRunShowsUpdateWithoutWriting(t *testing.T) {
	_, fixture, alphaDir, _ := importFixture(t)
	store, err := session.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	saveRecord(t, store, "alpha", alphaDir, "stale-alpha")

	out, _, err := runImportCmd(t, "--dry-run", "--from", fixture, "--no-open")
	if err != nil {
		t.Fatalf("execute: %v\n%s", err, out)
	}
	if got := row(out, "update", "alpha"); len(got) != 4 || got[3] != alphaID[:8] {
		t.Errorf("alpha row = %v in:\n%s", got, out)
	}
	if !strings.Contains(out,
		"ks: dry run, nothing written: 1 to import, 1 to update, 0 already present, 2 skipped") {
		t.Errorf("summary missing in:\n%s", out)
	}
	got, err := store.Load("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if got.ClaudeSessionID != "stale-alpha" || got.ClaudeTranscriptPath != "/old/stale-alpha.jsonl" {
		t.Errorf("dry run changed the record: %+v", got)
	}
}

func TestPlanImportMatchesByIDThenDirectory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const alphaDir, betaDir = "/code/alpha", "/code/beta"
	alpha := herdr.Agent{Dir: alphaDir, SessionID: alphaID}
	alphaAgain := herdr.Agent{Dir: alphaDir, SessionID: betaID}
	early, late := time.Unix(100, 0).UTC(), time.Unix(200, 0).UTC()
	rec := func(name, dir, claudeID string) *session.Session {
		s := session.New(name, dir, 0, 0)
		s.ClaudeSessionID = claudeID
		return s
	}
	focused := func(s *session.Session, at time.Time) *session.Session {
		s.FocusedAt = at
		return s
	}
	created := func(s *session.Session, at string) *session.Session {
		s.CreatedAt = at
		return s
	}
	stopped := rec("alpha", alphaDir, "stale")
	stopped.Status = session.StatusStopped

	tests := []struct {
		name     string
		agents   []herdr.Agent
		existing []*session.Session
		// open names the records whose claude window is open.
		open map[string]bool
		// want is "<label> <name>" per agent, in agent order.
		want []string
	}{
		{
			name:     "id match wins over the directory",
			agents:   []herdr.Agent{alpha},
			existing: []*session.Session{rec("alpha", alphaDir, alphaID)},
			want:     []string{"exists alpha"},
		},
		{
			name:     "directory match with a new id updates",
			agents:   []herdr.Agent{alpha},
			existing: []*session.Session{rec("alpha", alphaDir, "stale")},
			want:     []string{"updated alpha"},
		},
		{
			name:     "open record is skipped",
			agents:   []herdr.Agent{alpha},
			existing: []*session.Session{rec("alpha", alphaDir, "stale")},
			open:     map[string]bool{"alpha": true},
			want:     []string{"skipped alpha"},
		},
		{
			name:   "record matched by id is never the directory candidate",
			agents: []herdr.Agent{alpha, alphaAgain},
			existing: []*session.Session{
				focused(rec("alpha", alphaDir, alphaID), late),
				focused(rec("alpha-2", alphaDir, "stale"), early),
			},
			want: []string{"exists alpha", "updated alpha-2"},
		},
		{
			name:   "most recently focused candidate wins",
			agents: []herdr.Agent{alpha},
			existing: []*session.Session{
				focused(rec("alpha-2", alphaDir, "stale-2"), late),
				focused(rec("alpha", alphaDir, "stale-1"), early),
			},
			want: []string{"updated alpha-2"},
		},
		{
			name:   "newest created wins a focus tie",
			agents: []herdr.Agent{alpha},
			existing: []*session.Session{
				created(rec("alpha", alphaDir, "stale-1"), "2026-10-01T00:00:00Z"),
				created(rec("alpha-2", alphaDir, "stale-2"), "2026-10-02T00:00:00.5Z"),
			},
			want: []string{"updated alpha-2"},
		},
		{
			name:     "stopped record for the directory is updated",
			agents:   []herdr.Agent{alpha},
			existing: []*session.Session{stopped},
			want:     []string{"updated alpha"},
		},
		{
			name:   "active record is preferred over a stopped one",
			agents: []herdr.Agent{alpha},
			existing: []*session.Session{
				focused(stopped, late),
				focused(rec("alpha-2", alphaDir, "stale-2"), early),
			},
			want: []string{"updated alpha-2"},
		},
		{
			name:     "each stale record is claimed once",
			agents:   []herdr.Agent{alpha, alphaAgain},
			existing: []*session.Session{rec("alpha", alphaDir, "stale")},
			want:     []string{"updated alpha", "imported alpha-2"},
		},
		{
			name:     "a record for another directory is not a match",
			agents:   []herdr.Agent{alpha},
			existing: []*session.Session{rec("beta", betaDir, "stale")},
			want:     []string{"imported alpha"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			open := func(s *session.Session) bool { return tc.open[s.Name] }
			items, err := planImport(tc.agents, tc.existing, open)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, it := range items {
				got = append(got, it.action.label(false)+" "+it.name)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("plan = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestImportOpenRecordRowNamesTheReason(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	alpha := herdr.Agent{Dir: "/code/alpha", SessionID: alphaID}
	existing := session.New("alpha", alpha.Dir, 0, 0)
	existing.ClaudeSessionID = "stale"
	items, err := planImport(
		[]herdr.Agent{alpha},
		[]*session.Session{existing},
		func(*session.Session) bool { return true },
	)
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := printImportRows(&out, items, nil, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "alpha is open in ks, not updated") {
		t.Errorf("rows = %q", out.String())
	}
	if warnings := transcriptWarnings(items); len(warnings) != 0 {
		t.Errorf("a record left alone should not warn: %v", warnings)
	}
}

func TestImportReactivatesStoppedRecord(t *testing.T) {
	_, fixture, alphaDir, _ := importFixture(t)
	store, err := session.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	old := saveRecord(t, store, "alpha", alphaDir, "stale-alpha")
	old.Status = session.StatusStopped
	if err := store.Save(old); err != nil {
		t.Fatal(err)
	}

	out, _, err := runImportCmd(t, "--from", fixture, "--no-open")
	if err != nil {
		t.Fatalf("execute: %v\n%s", err, out)
	}
	if got := row(out, "updated", "alpha"); len(got) != 4 || got[3] != alphaID[:8] {
		t.Errorf("alpha row = %v in:\n%s", got, out)
	}
	got, err := store.Load("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != old.ID || got.ClaudeSessionID != alphaID || got.Status != session.StatusActive {
		t.Errorf("alpha record = %+v", got)
	}
	if store.Exists("alpha-2") {
		t.Error("a stopped record for the directory must not get a -2 twin")
	}
}
