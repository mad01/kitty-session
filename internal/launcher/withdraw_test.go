package launcher

import (
	"errors"
	"slices"
	"testing"

	"github.com/mad01/kitty-session/internal/kitty"
	"github.com/mad01/kitty-session/internal/session"
)

// newWithdrawFixture opens one, two and three, drops three's sidebar window,
// and adds gone, an active record with no window. A fresh one per subtest
// keeps the subtests order-independent. one is sidebar 2, two is sidebar 4.
func newWithdrawFixture(t *testing.T) (*Launcher, *fakeKitty, *session.Store) {
	t.Helper()
	l, f, store := newTestLauncher(t)
	for _, name := range []string{"one", "two", "three"} {
		if _, err := l.Open(Request{Name: name, Dir: "/work/" + name}); err != nil {
			t.Fatal(err)
		}
	}
	three, err := store.Load("three")
	if err != nil {
		t.Fatal(err)
	}
	dropWindow(f, three.KittySidebarWindowID)
	if err := store.Save(session.New("gone", "/work/gone", 70, 71)); err != nil {
		t.Fatal(err)
	}
	f.calls, f.slept = nil, 0
	return l, f, store
}

func TestWithdrawBanners(t *testing.T) {
	t.Run("signals each live sidebar, then waits until their tabs go", func(t *testing.T) {
		l, f, store := newWithdrawFixture(t)
		sessions, err := store.List()
		if err != nil {
			t.Fatal(err)
		}
		if w := l.WithdrawBanners(sessions, ""); len(w) != 0 {
			t.Errorf("warnings = %v, want none", w)
		}
		// one (sidebar 2) and two (sidebar 4) are signalled and close their
		// tabs; three has no sidebar and gone no window. The wait then sees
		// no window left tagged with those sessions on its first poll.
		want := []string{"Windows", "SignalChild(2,SIGUSR1)", "SignalChild(4,SIGUSR1)", "Windows"}
		if !slices.Equal(f.calls, want) {
			t.Errorf("calls = %v, want %v", f.calls, want)
		}
		if f.slept != 0 {
			t.Errorf("slept %v, want 0: the tabs are gone on the first poll", f.slept)
		}
	})
	t.Run("skips exceptID", func(t *testing.T) {
		l, f, store := newWithdrawFixture(t)
		sessions, err := store.List()
		if err != nil {
			t.Fatal(err)
		}
		two, err := store.Load("two")
		if err != nil {
			t.Fatal(err)
		}
		if w := l.WithdrawBanners(sessions, two.ID); len(w) != 0 {
			t.Errorf("warnings = %v, want none", w)
		}
		if slices.Contains(f.calls, "SignalChild(4,SIGUSR1)") {
			t.Errorf("exceptID's sidebar was signalled: %v", f.calls)
		}
		if !slices.Contains(f.calls, "SignalChild(2,SIGUSR1)") {
			t.Errorf("the other sidebar was not signalled: %v", f.calls)
		}
	})
	t.Run("a failed signal is a warning, the rest are still signalled", func(t *testing.T) {
		l, f, store := newWithdrawFixture(t)
		f.errs["SignalChild(2,SIGUSR1)"] = errors.New("no such process")
		sessions, err := store.List()
		if err != nil {
			t.Fatal(err)
		}
		if w := l.WithdrawBanners(sessions, ""); len(w) != 1 {
			t.Errorf("warnings = %v, want 1", w)
		}
		if !slices.Contains(f.calls, "SignalChild(4,SIGUSR1)") {
			t.Errorf("the second sidebar was not signalled after the first failed: %v", f.calls)
		}
	})
	t.Run("an unreachable instance is a warning and no signal", func(t *testing.T) {
		l, f, store := newWithdrawFixture(t)
		f.errs["Windows"] = errors.New("connection refused")
		sessions, err := store.List()
		if err != nil {
			t.Fatal(err)
		}
		if w := l.WithdrawBanners(sessions, ""); len(w) != 1 {
			t.Errorf("warnings = %v, want 1", w)
		}
		if hasPrefixCall(f, "SignalChild(") {
			t.Errorf("signalled despite the snapshot failing: %v", f.calls)
		}
	})
}

func TestAwaitClosed(t *testing.T) {
	polls := int(closeGrace / closePoll)
	t.Run("returns at once when no window carries the ids", func(t *testing.T) {
		l, f, store := newTestLauncher(t)
		sess := session.New("s", "/work/s", 0, 0)
		f.addTab(sess, true)
		if err := store.Save(sess); err != nil {
			t.Fatal(err)
		}
		f.calls, f.slept = nil, 0
		l.awaitClosed(slices.Clone(f.windows), []string{"no-such-session"})
		if n := countCalls(f, "Windows"); n != 1 {
			t.Errorf("Windows called %d times, want 1", n)
		}
		if f.slept != 0 {
			t.Errorf("slept %v, want 0", f.slept)
		}
	})
	t.Run("waits the cap while the windows persist", func(t *testing.T) {
		l, f, store := newTestLauncher(t)
		sess := session.New("s", "/work/s", 0, 0)
		f.addTab(sess, true)
		if err := store.Save(sess); err != nil {
			t.Fatal(err)
		}
		f.calls, f.slept = nil, 0
		l.awaitClosed(slices.Clone(f.windows), []string{sess.ID})
		if n := countCalls(f, "Windows"); n != polls+1 {
			t.Errorf("Windows called %d times, want %d", n, polls+1)
		}
		if f.slept != closeGrace {
			t.Errorf("slept %v, want %v", f.slept, closeGrace)
		}
	})
	t.Run("a poll error keeps the previous snapshot and waits out the cap", func(t *testing.T) {
		l, f, store := newTestLauncher(t)
		sess := session.New("s", "/work/s", 0, 0)
		f.addTab(sess, true)
		if err := store.Save(sess); err != nil {
			t.Fatal(err)
		}
		prev := slices.Clone(f.windows)
		f.calls, f.slept = nil, 0
		f.onWindows = func() error { return errors.New("ls broke") }
		got := l.awaitClosed(prev, []string{sess.ID})
		if f.slept != closeGrace {
			t.Errorf("slept %v, want the cap %v", f.slept, closeGrace)
		}
		if !slices.EqualFunc(got, prev, func(a, b kitty.Window) bool { return a.ID == b.ID }) {
			t.Errorf("returned snapshot = %v, want the previous one", got)
		}
	})
}

// TestQuitSignalsOthersAndWithdrawsOwn checks that Quit withdraws its own
// banner inline and signals every other session's sidebar before the quit
// func runs.
func TestQuitSignalsOthersAndWithdrawsOwn(t *testing.T) {
	b, f, _ := newTestBackend(t)
	a := session.New("a", "/work/a", 0, 0)
	other := session.New("other", "/work/other", 0, 0)
	for _, sess := range []*session.Session{a, other} {
		f.addTab(sess, true)
		if err := b.l.store.Save(sess); err != nil {
			t.Fatal(err)
		}
	}
	b.ownID = a.ID
	withdrew := false
	b.withdraw = func() { withdrew = true }
	quitAt := -1
	b.quit = func() error { quitAt = len(f.calls); return nil }
	f.calls = nil
	if err := b.Quit(); err != nil {
		t.Fatalf("Quit: %v", err)
	}
	if !withdrew {
		t.Error("Quit did not withdraw its own banner")
	}
	if quitAt < 0 {
		t.Fatal("quit func was not called")
	}
	ownSignal := "SignalChild(" + itoa(a.KittySidebarWindowID) + ",SIGUSR1)"
	otherSignal := "SignalChild(" + itoa(other.KittySidebarWindowID) + ",SIGUSR1)"
	if slices.Contains(f.calls, ownSignal) {
		t.Error("Quit signalled its own sidebar instead of withdrawing inline")
	}
	i := slices.Index(f.calls, otherSignal)
	if i < 0 {
		t.Errorf("Quit did not signal the other sidebar: %v", f.calls)
	} else if i >= quitAt {
		t.Errorf("the signal at %d ran at or after the quit (%d)", i, quitAt)
	}
}
