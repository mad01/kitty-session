package cli

import (
	"strings"
	"testing"

	"github.com/mad01/kitty-session/internal/session"
)

// TestListShowsTheKind runs ks list against a store under a fake HOME with
// no instance, so every row is stopped and the kind column is what varies.
func TestListShowsTheKind(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store, err := session.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	old := session.New("ks-list-old", "/work/old", 0, 0) // no agent field: claude
	pi := session.New("ks-list-pi", "/work/pi", 0, 0)
	pi.Agent = session.AgentPi
	sh := session.New("ks-list-sh", "/work/sh", 0, 0)
	sh.Agent = session.AgentShell
	for _, s := range []*session.Session{old, pi, sh} {
		if err := store.Save(s); err != nil {
			t.Fatal(err)
		}
	}

	out, err := runCmd(t, "list")
	if err != nil {
		t.Fatalf("list returned %v\noutput: %s", err, out)
	}
	for _, want := range []string{
		"ks-list-old          claude  stopped    /work/old",
		"ks-list-pi           pi      stopped    /work/pi",
		"ks-list-sh           shell   stopped    /work/sh",
		"ks instance not running",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}
