package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/mad01/kitty-session/internal/session"
)

// TestRenameSelf covers the one-argument form: the record is found by
// KS_SESSION_ID even though KS_SESSION_NAME still carries an older name. The
// names are prefixed so the state-file rename, whose directory is resolved
// once per process, cannot touch a real session's file.
func TestRenameSelf(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store, err := session.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	sess := session.New("ks-rename-test-old", "/work/demo", 0, 0)
	if err := store.Save(sess); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KS_SESSION_ID", sess.ID)
	t.Setenv("KS_SESSION_NAME", "ks-rename-test-stale")

	out, err := runCmd(t, "rename", "ks-rename-test-new")
	if err != nil {
		t.Fatalf("rename returned %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, `session "ks-rename-test-old" renamed to "ks-rename-test-new"`) {
		t.Errorf("output = %q, want the renamed line", out)
	}
	if store.Exists("ks-rename-test-old") {
		t.Error("old record still there")
	}
	got, err := store.Load("ks-rename-test-new")
	if err != nil {
		t.Fatalf("new record: %v", err)
	}
	if got.ID != sess.ID {
		t.Errorf("renamed record has id %q, want %q", got.ID, sess.ID)
	}
}

func TestRenameOutsideASession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KS_SESSION_ID", "")
	t.Setenv("KS_SESSION_NAME", "")

	_, err := runCmd(t, "rename", "new")
	if !errors.Is(err, errNotInSession) {
		t.Fatalf("rename returned %v, want errNotInSession", err)
	}
}
