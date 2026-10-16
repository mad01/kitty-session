package launcher

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/mad01/kitty-session/internal/session"
	"github.com/mad01/kitty-session/internal/state"
)

// TestClose is the only test in this package that touches state files:
// state.Dir pins HOME on first use, so every subtest shares one HOME and
// store; a second top-level test with its own HOME would write into a
// removed directory. The subtests reuse the one launcher and reset the fake
// between them.
func TestClose(t *testing.T) {
	l, f, store := newTestLauncher(t)

	// open adds a session with a tab (sidebar + claude), writes a state file,
	// and resets the fake's recording. withSidebar false drops the sidebar
	// window, leaving only claude, as a crashed or relaunched sidebar would.
	open := func(name string, withSidebar bool) *session.Session {
		sess := session.New(name, "/work/demo", 0, 0)
		f.addTab(sess, true)
		if !withSidebar {
			dropWindow(f, sess.KittySidebarWindowID)
		}
		if err := store.Save(sess); err != nil {
			t.Fatal(err)
		}
		if err := state.Write(name, "idle"); err != nil {
			t.Fatal(err)
		}
		f.calls, f.slept, f.ignoreSignal = nil, 0, false
		delete(f.errs, "SignalChild")
		delete(f.errs, "CloseTab")
		delete(f.errs, "Windows")
		return sess
	}

	wantStopped := func(name string) {
		t.Helper()
		if _, _, err := state.Read(name); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s: state file still present (err %v)", name, err)
		}
		got, err := store.Load(name)
		if err != nil {
			t.Fatalf("%s: kept record missing: %v", name, err)
		}
		if got.Status != session.StatusStopped {
			t.Errorf("%s: Status = %q, want stopped", name, got.Status)
		}
	}

	t.Run("a modern sidebar closes its own tab; the launcher closes nothing", func(t *testing.T) {
		sess := open("modern", true)
		warnings, err := l.Close(sess, CloseOptions{Keep: true, Signal: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(warnings) != 0 {
			t.Errorf("warnings = %v, want none", warnings)
		}
		want := []string{
			"Windows", fmt.Sprintf("SignalChild(%d,SIGUSR1)", sess.KittySidebarWindowID), "Windows",
		}
		if !slices.Equal(f.calls, want) {
			t.Errorf("calls = %v, want %v", f.calls, want)
		}
		if hasPrefixCall(f, "CloseTab(") {
			t.Error("the launcher closed a tab the sidebar had already closed")
		}
		if f.slept != 0 {
			t.Errorf("slept %v, want 0: the tab is gone on the first poll", f.slept)
		}
		wantStopped("modern")
	})

	t.Run("delete trashes the record and skips a dead tab", func(t *testing.T) {
		sess := session.New("deleted", "/work/demo", 70, 71) // stale ids, nothing in kitty
		if err := store.Save(sess); err != nil {
			t.Fatal(err)
		}
		if err := state.Write("deleted", "idle"); err != nil {
			t.Fatal(err)
		}
		f.calls, f.slept = nil, 0
		warnings, err := l.Close(sess, CloseOptions{Signal: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(warnings) != 0 {
			t.Errorf("warnings = %v, want none", warnings)
		}
		if want := []string{"Windows"}; !slices.Equal(f.calls, want) {
			t.Errorf("calls = %v, want %v", f.calls, want)
		}
		if _, err := store.Load("deleted"); err == nil {
			t.Fatal("record still in the store, want trashed")
		}
		if trashed, err := store.ListTrashed(); err != nil || !containsName(trashed, "deleted") {
			t.Errorf("trash = %v, %v; want deleted present", trashed, err)
		}
	})

	t.Run("a tab with no live sidebar is closed directly", func(t *testing.T) {
		sess := open("bare", false)
		warnings, err := l.Close(sess, CloseOptions{Keep: true, Signal: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(warnings) != 0 {
			t.Errorf("warnings = %v, want none", warnings)
		}
		want := []string{"Windows", fmt.Sprintf("CloseTab(%d)", sess.KittyTabID)}
		if !slices.Equal(f.calls, want) {
			t.Errorf("calls = %v, want %v", f.calls, want)
		}
		wantStopped("bare")
	})

	t.Run("closing the own session sends no signal and closes directly", func(t *testing.T) {
		sess := open("self", true)
		warnings, err := l.Close(sess, CloseOptions{Keep: true, Signal: false})
		if err != nil {
			t.Fatal(err)
		}
		if len(warnings) != 0 {
			t.Errorf("warnings = %v, want none", warnings)
		}
		if hasPrefixCall(f, "SignalChild(") {
			t.Error("the own-session close signalled its own sidebar")
		}
		want := []string{"Windows", fmt.Sprintf("CloseTab(%d)", sess.KittyTabID)}
		if !slices.Equal(f.calls, want) {
			t.Errorf("calls = %v, want %v", f.calls, want)
		}
		wantStopped("self")
	})

	t.Run("an older sidebar that ignores the signal is waited out, then closed", func(t *testing.T) {
		sess := open("older", true)
		f.ignoreSignal = true // the sidebar never closes its tab
		warnings, err := l.Close(sess, CloseOptions{Keep: true, Signal: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(warnings) != 0 {
			t.Errorf("warnings = %v, want none", warnings)
		}
		if !slices.Contains(f.calls, fmt.Sprintf("SignalChild(%d,SIGUSR1)", sess.KittySidebarWindowID)) {
			t.Errorf("calls = %v, want a SignalChild", f.calls)
		}
		if !slices.Contains(f.calls, fmt.Sprintf("CloseTab(%d)", sess.KittyTabID)) {
			t.Errorf("calls = %v, want the leftover tab closed", f.calls)
		}
		if f.slept != closeGrace {
			t.Errorf("slept %v, want the cap %v", f.slept, closeGrace)
		}
		wantStopped("older")
	})

	t.Run("a sidebar that cannot be signalled is a warning, the tab still closes", func(t *testing.T) {
		sess := open("deaf", true)
		f.errs["SignalChild"] = errors.New("no such process")
		warnings, err := l.Close(sess, CloseOptions{Keep: true, Signal: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(warnings) != 1 {
			t.Errorf("warnings = %v, want 1", warnings)
		}
		want := []string{
			"Windows",
			fmt.Sprintf("SignalChild(%d,SIGUSR1)", sess.KittySidebarWindowID),
			fmt.Sprintf("CloseTab(%d)", sess.KittyTabID),
		}
		if !slices.Equal(f.calls, want) {
			t.Errorf("calls = %v, want %v", f.calls, want)
		}
		if f.slept != 0 {
			t.Errorf("slept %v, want 0: a failed signal is not awaited", f.slept)
		}
		wantStopped("deaf")
	})

	t.Run("an unreachable instance is a warning, the record still stops", func(t *testing.T) {
		sess := open("offline", true)
		f.errs["Windows"] = errors.New("connection refused")
		warnings, err := l.Close(sess, CloseOptions{Keep: true, Signal: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(warnings) != 1 {
			t.Errorf("warnings = %v, want 1", warnings)
		}
		wantStopped("offline")
	})
}

// containsName reports whether any session in the list has the name.
func containsName(sessions []*session.Session, name string) bool {
	return slices.ContainsFunc(sessions, func(s *session.Session) bool { return s.Name == name })
}
