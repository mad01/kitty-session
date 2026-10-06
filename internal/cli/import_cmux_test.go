package cli

import (
	"errors"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mad01/kitty-session/internal/cmux"
)

// fakeOpener records the workspaces it is asked to create and fails the
// ones named in fail.
type fakeOpener struct {
	opened []cmux.Workspace
	fail   map[string]bool
}

func (f *fakeOpener) NewWorkspace(ws cmux.Workspace) error {
	if f.fail[ws.Name] {
		return errors.New("cmux new-workspace " + ws.Name + ": boom")
	}
	f.opened = append(f.opened, ws)
	return nil
}

// useFakeCmux swaps the cmux seams for a fake opener and the given
// inside-cmux answer, restoring them when the test ends.
func useFakeCmux(t *testing.T, inside bool) *fakeOpener {
	t.Helper()
	fake := &fakeOpener{fail: map[string]bool{}}
	prevOpener, prevInside := newWorkspaceOpener, insideCmux
	newWorkspaceOpener = func() (workspaceOpener, error) { return fake, nil }
	insideCmux = func() bool { return inside }
	t.Cleanup(func() { newWorkspaceOpener, insideCmux = prevOpener, prevInside })
	return fake
}

func TestImportToCmuxOpensResumingWorkspaces(t *testing.T) {
	home, fixture, alphaDir, betaDir := importFixture(t)
	writeTranscript(t, alphaDir, alphaID)
	fake := useFakeCmux(t, true)

	out, errOut, err := runImportCmd(t, "--to", "cmux", "--from", fixture)
	if err != nil {
		t.Fatalf("execute: %v\n%s%s", err, out, errOut)
	}
	if len(fake.opened) != 2 {
		t.Fatalf("opened %d workspaces, want 2: %+v", len(fake.opened), fake.opened)
	}
	alpha, beta := fake.opened[0], fake.opened[1]
	if alpha.Name != "alpha" || alpha.Dir != alphaDir ||
		strings.Join(alpha.Command, " ") != "claude --resume "+alphaID {
		t.Errorf("alpha workspace = %+v", alpha)
	}
	// beta has no transcript and its directory none either: a fresh claude.
	if beta.Name != "beta-work" || beta.Dir != betaDir ||
		strings.Join(beta.Command, " ") != "claude" {
		t.Errorf("beta workspace = %+v", beta)
	}
	if !strings.Contains(out, "ks: 2 opened in cmux, 2 skipped") {
		t.Errorf("summary missing in:\n%s", out)
	}
	if !strings.Contains(errOut, "beta-work: transcript for "+betaID[:8]+" missing") {
		t.Errorf("beta warning missing in stderr:\n%s", errOut)
	}
	if files := sessionFiles(t, home); len(files) != 0 {
		t.Errorf("cmux import must write no ks records, wrote %v", files)
	}
}

func TestImportToCmuxDryRunOpensNothing(t *testing.T) {
	_, fixture, _, _ := importFixture(t)
	fake := useFakeCmux(t, false)

	out, _, err := runImportCmd(t, "--to", "cmux", "--dry-run", "--from", fixture)
	if err != nil {
		t.Fatalf("execute: %v\n%s", err, out)
	}
	if len(fake.opened) != 0 {
		t.Errorf("dry run opened %+v", fake.opened)
	}
	if got := row(out, "open", "alpha"); len(got) < 4 || got[3] != "claude" {
		t.Errorf("alpha row = %v", got)
	}
	if !strings.Contains(out, "ks: dry run, nothing opened: 2 to open, 2 skipped") {
		t.Errorf("summary missing in:\n%s", out)
	}
}

func TestImportToCmuxRefusesOutsideCmux(t *testing.T) {
	_, fixture, _, _ := importFixture(t)
	fake := useFakeCmux(t, false)

	_, _, err := runImportCmd(t, "--to", "cmux", "--from", fixture)
	if err == nil || !strings.Contains(err.Error(), "inside cmux") {
		t.Fatalf("err = %v, want the run-inside-cmux error", err)
	}
	if len(fake.opened) != 0 {
		t.Errorf("opened %+v outside cmux", fake.opened)
	}
}

func TestImportToCmuxLeavesAgentsWhileHerdrRuns(t *testing.T) {
	_, fixture, _, _ := importFixture(t)
	l, err := net.Listen("unix", filepath.Join(filepath.Dir(fixture), "herdr.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	fake := useFakeCmux(t, true)

	_, errOut, err := runImportCmd(t, "--to", "cmux", "--from", fixture)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(fake.opened) != 0 {
		t.Errorf("opened %+v while herdr runs them", fake.opened)
	}
	if !strings.Contains(errOut, "herdr is still running") {
		t.Errorf("expected the herdr-running note, got:\n%s", errOut)
	}
}

func TestImportToCmuxContinuesPastAFailure(t *testing.T) {
	_, fixture, _, _ := importFixture(t)
	fake := useFakeCmux(t, true)
	fake.fail["alpha"] = true

	out, _, err := runImportCmd(t, "--to", "cmux", "--from", fixture)
	if err == nil || !strings.Contains(err.Error(), "alpha: boom") {
		t.Fatalf("err = %v, want alpha's failure", err)
	}
	if len(fake.opened) != 1 || fake.opened[0].Name != "beta-work" {
		t.Errorf("opened %+v, want beta-work only", fake.opened)
	}
	if !strings.Contains(out, "ks: 1 opened in cmux") {
		t.Errorf("summary missing in:\n%s", out)
	}
}

func TestImportRejectsUnknownTarget(t *testing.T) {
	_, fixture, _, _ := importFixture(t)

	_, _, err := runImportCmd(t, "--to", "tmux", "--from", fixture)
	if err == nil || !strings.Contains(err.Error(), `unknown --to "tmux"`) {
		t.Fatalf("err = %v, want unknown target", err)
	}
}
