package launcher

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/mad01/kitty-session/internal/kitty"
	"github.com/mad01/kitty-session/internal/session"
)

// moveFixture is an instance as it looks while sessions run: no home tab,
// tabs a, b, c with a sidebar and claude each (windows 1+2, 3+4, 5+6), tab d
// with only its sidebar (window 7), the keyboard on b's claude. The store
// also holds "stopped", a stopped record ranked 5, and "ghost", an active
// record whose tab is gone; neither has a window.
func moveFixture(t *testing.T) (*Launcher, *fakeKitty, *session.Store) {
	t.Helper()
	l, f, store := newTestLauncher(t)
	f.windows, f.nextWindow, f.nextTab = nil, 1, 1
	for _, name := range []string{"a", "b", "c", "d"} {
		sess := session.New(name, "/work/"+name, 0, 0)
		f.addTab(sess, name != "d")
		if name == "d" {
			sess.Position = 9 // a stale rank, to be renumbered
		}
		if err := store.Save(sess); err != nil {
			t.Fatal(err)
		}
	}
	stopped := session.New("stopped", "/work/stopped", 0, 0)
	stopped.Status, stopped.Position = session.StatusStopped, 5
	ghost := session.New("ghost", "/work/ghost", 70, 71)
	for _, sess := range []*session.Session{stopped, ghost} {
		if err := store.Save(sess); err != nil {
			t.Fatal(err)
		}
	}
	f.focus(4)
	f.calls = nil
	return l, f, store
}

func TestMove(t *testing.T) {
	// unranked is what the fixture's records carry before any move: only
	// the two without a tab have a rank, and keep it.
	unranked := map[string]int{"a": 0, "b": 0, "c": 0, "d": 9, "stopped": 5, "ghost": 0}
	tests := []struct {
		name      string
		session   string
		where     MoveTarget
		blurred   bool // the OS window has no keyboard focus
		wantFrom  int
		wantTo    int
		wantOrder string // tab order after, by session name
		wantCalls []string
		wantRanks map[string]int
	}{
		{
			name: "to top from behind: shown, moved, keyboard given back",
			session: "c", where: MoveTarget{Kind: MoveTop},
			wantFrom: 3, wantTo: 1, wantOrder: "c a b d",
			wantCalls: []string{
				"Windows", "FocusWindow(6)", "MoveActiveTab(-2)", "FocusWindow(4)", "Windows",
			},
			wantRanks: map[string]int{"c": 1, "a": 2, "b": 3, "d": 4, "stopped": 5, "ghost": 0},
		},
		{
			name: "to bottom", session: "a", where: MoveTarget{Kind: MoveBottom},
			wantFrom: 1, wantTo: 4, wantOrder: "b c d a",
			wantCalls: []string{
				"Windows", "FocusWindow(2)", "MoveActiveTab(3)", "FocusWindow(4)", "Windows",
			},
			wantRanks: map[string]int{"b": 1, "c": 2, "d": 3, "a": 4, "stopped": 5, "ghost": 0},
		},
		{
			name: "the showing tab moves up without any focus change",
			session: "b", where: MoveTarget{Kind: MoveUp},
			wantFrom: 2, wantTo: 1, wantOrder: "b a c d",
			wantCalls: []string{"Windows", "MoveActiveTab(-1)", "Windows"},
			wantRanks: map[string]int{"b": 1, "a": 2, "c": 3, "d": 4, "stopped": 5, "ghost": 0},
		},
		{
			name: "down", session: "b", where: MoveTarget{Kind: MoveDown},
			wantFrom: 2, wantTo: 3, wantOrder: "a c b d",
			wantCalls: []string{"Windows", "MoveActiveTab(1)", "Windows"},
			wantRanks: map[string]int{"a": 1, "c": 2, "b": 3, "d": 4, "stopped": 5, "ghost": 0},
		},
		{
			name: "to a position", session: "a", where: MoveTarget{Kind: MoveTo, Position: 3},
			wantFrom: 1, wantTo: 3, wantOrder: "b c a d",
			wantCalls: []string{
				"Windows", "FocusWindow(2)", "MoveActiveTab(2)", "FocusWindow(4)", "Windows",
			},
			wantRanks: map[string]int{"b": 1, "c": 2, "a": 3, "d": 4, "stopped": 5, "ghost": 0},
		},
		{
			name: "a sidebar-only tab is shown through its sidebar",
			session: "d", where: MoveTarget{Kind: MoveTo, Position: 2},
			wantFrom: 4, wantTo: 2, wantOrder: "a d b c",
			wantCalls: []string{
				"Windows", "FocusWindow(7)", "MoveActiveTab(-2)", "FocusWindow(4)", "Windows",
			},
			wantRanks: map[string]int{"a": 1, "d": 2, "b": 3, "c": 4, "stopped": 5, "ghost": 0},
		},
		{
			name: "a position past the end means the end",
			session: "a", where: MoveTarget{Kind: MoveTo, Position: 40},
			wantFrom: 1, wantTo: 4, wantOrder: "b c d a",
			wantCalls: []string{
				"Windows", "FocusWindow(2)", "MoveActiveTab(3)", "FocusWindow(4)", "Windows",
			},
			wantRanks: map[string]int{"b": 1, "c": 2, "d": 3, "a": 4, "stopped": 5, "ghost": 0},
		},
		{
			name: "already there: nothing asked of kitty, nothing ranked",
			session: "a", where: MoveTarget{Kind: MoveTop},
			wantFrom: 1, wantTo: 1, wantOrder: "a b c d",
			wantCalls: []string{"Windows"},
			wantRanks: unranked,
		},
		{
			name: "up from the top stays", session: "a", where: MoveTarget{Kind: MoveUp},
			wantFrom: 1, wantTo: 1, wantOrder: "a b c d",
			wantCalls: []string{"Windows"},
			wantRanks: unranked,
		},
		{
			name: "down from the bottom stays", session: "d", where: MoveTarget{Kind: MoveDown},
			wantFrom: 4, wantTo: 4, wantOrder: "a b c d",
			wantCalls: []string{"Windows"},
			wantRanks: unranked,
		},
		{
			name: "without keyboard focus the showing tab's first window gets it back",
			session: "c", where: MoveTarget{Kind: MoveTop}, blurred: true,
			wantFrom: 3, wantTo: 1, wantOrder: "c a b d",
			wantCalls: []string{
				"Windows", "FocusWindow(6)", "MoveActiveTab(-2)", "FocusWindow(3)", "Windows",
			},
			wantRanks: map[string]int{"c": 1, "a": 2, "b": 3, "d": 4, "stopped": 5, "ghost": 0},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l, f, store := moveFixture(t)
			if tc.blurred {
				for i := range f.windows {
					f.windows[i].Focused = false
				}
			}

			res, err := l.Move(tc.session, tc.where)
			if err != nil {
				t.Fatalf("Move: %v", err)
			}
			if res.From != tc.wantFrom || res.To != tc.wantTo {
				t.Errorf("moved %d → %d, want %d → %d", res.From, res.To, tc.wantFrom, tc.wantTo)
			}
			if len(res.Warnings) != 0 {
				t.Errorf("warnings = %v, want none", res.Warnings)
			}
			if got := tabOrderByName(f.windows); got != tc.wantOrder {
				t.Errorf("tab order = %q, want %q", got, tc.wantOrder)
			}
			if !slices.Equal(f.calls, tc.wantCalls) {
				t.Errorf("calls = %v, want %v", f.calls, tc.wantCalls)
			}
			checkRanks(t, store, tc.wantRanks)
		})
	}
}

func TestMoveRefusals(t *testing.T) {
	tests := []struct {
		name      string
		session   string
		lsErr     error
		wantErr   string
		wantCalls []string
	}{
		{
			name: "unknown session", session: "nobody",
			wantErr: `session "nobody" not found`,
		},
		{
			name: "stopped session is refused before kitty is asked", session: "stopped",
			wantErr: `session "stopped" has no open tab`,
		},
		{
			name: "active record whose tab is gone", session: "ghost",
			wantErr: `session "ghost" has no open tab`, wantCalls: []string{"Windows"},
		},
		{
			name: "instance down", session: "a", lsErr: errors.New("connection refused"),
			wantErr: "cannot list kitty windows: connection refused", wantCalls: []string{"Windows"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l, f, store := moveFixture(t)
			f.errs["Windows"] = tc.lsErr

			_, err := l.Move(tc.session, MoveTarget{Kind: MoveTop})
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Move error = %v, want %q", err, tc.wantErr)
			}
			if !slices.Equal(f.calls, tc.wantCalls) {
				t.Errorf("calls = %v, want %v", f.calls, tc.wantCalls)
			}
			if got := tabOrderByName(f.windows); got != "a b c d" {
				t.Errorf("tab order changed to %q", got)
			}
			checkRanks(t, store, map[string]int{"a": 0, "b": 0, "c": 0, "d": 9, "stopped": 5, "ghost": 0})
		})
	}
}

// TestMoveWarnsWhenTheOrderCannotBeRecorded covers the snapshot after the
// move failing: the tab has moved, the keyboard is back, and the ranks are
// as they were.
func TestMoveWarnsWhenTheOrderCannotBeRecorded(t *testing.T) {
	l, f, store := moveFixture(t)
	calls := 0
	f.errs = map[string]error{}
	lsFails := errors.New("gone away")
	f.onWindows = func() error {
		calls++
		if calls == 2 {
			return lsFails
		}
		return nil
	}

	res, err := l.Move("c", MoveTarget{Kind: MoveTop})
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if len(res.Warnings) != 1 || !errors.Is(res.Warnings[0], lsFails) {
		t.Errorf("warnings = %v, want one wrapping the ls failure", res.Warnings)
	}
	if got := tabOrderByName(f.windows); got != "c a b d" {
		t.Errorf("tab order = %q, want c a b d", got)
	}
	checkRanks(t, store, map[string]int{"a": 0, "b": 0, "c": 0, "d": 9, "stopped": 5, "ghost": 0})
}

func TestMoveTargetDestination(t *testing.T) {
	tests := []struct {
		name   string
		target MoveTarget
		from   int
		want   int
	}{
		{"top", MoveTarget{Kind: MoveTop}, 3, 1},
		{"bottom", MoveTarget{Kind: MoveBottom}, 1, 4},
		{"up", MoveTarget{Kind: MoveUp}, 3, 2},
		{"up at the top", MoveTarget{Kind: MoveUp}, 1, 1},
		{"down", MoveTarget{Kind: MoveDown}, 3, 4},
		{"down at the bottom", MoveTarget{Kind: MoveDown}, 4, 4},
		{"position", MoveTarget{Kind: MoveTo, Position: 2}, 4, 2},
		{"position past the end", MoveTarget{Kind: MoveTo, Position: 9}, 1, 4},
		{"position zero", MoveTarget{Kind: MoveTo}, 2, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.target.destination(tc.from, 4); got != tc.want {
				t.Errorf("destination(%d, 4) = %d, want %d", tc.from, got, tc.want)
			}
		})
	}
}

// tabOrderByName lists the fake's tabs in order by the session name each
// tab is titled with.
func tabOrderByName(all []kitty.Window) string {
	var names []string
	var seen []int
	for _, w := range all {
		if slices.Contains(seen, w.TabID) {
			continue
		}
		seen = append(seen, w.TabID)
		names = append(names, w.TabTitle)
	}
	return strings.Join(names, " ")
}

// checkRanks compares every record's Position with want.
func checkRanks(t *testing.T, store *session.Store, want map[string]int) {
	t.Helper()
	for name, rank := range want {
		sess, err := store.Load(name)
		if err != nil {
			t.Fatal(err)
		}
		if sess.Position != rank {
			t.Errorf("%s Position = %d, want %d", name, sess.Position, rank)
		}
	}
}

// TestLaunchReranks covers the rank upkeep a launch does: once a rank exists,
// a reopened session takes the bottom rank and the others keep theirs, at the
// cost of one more snapshot; a store that never saw a ks move stays unranked
// and costs nothing.
func TestLaunchReranks(t *testing.T) {
	tests := []struct {
		name      string
		ranks     map[string]int // a and b have tabs; c is stopped without one
		wantRanks map[string]int // after reopening c
		wantExtra []string       // calls after the launch and the home tab's close
	}{
		{
			name:      "a reopened session takes the bottom rank",
			ranks:     map[string]int{"a": 1, "b": 2, "c": 1},
			wantRanks: map[string]int{"a": 1, "b": 2, "c": 3},
			wantExtra: []string{"Windows"},
		},
		{
			name:      "an unranked store stays unranked",
			ranks:     map[string]int{},
			wantRanks: map[string]int{"a": 0, "b": 0, "c": 0},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l, f, store := newTestLauncher(t)
			for _, name := range []string{"a", "b", "c"} {
				sess := session.New(name, "/work/"+name, 0, 0)
				sess.Position = tc.ranks[name]
				if name == "c" {
					sess.Status = session.StatusStopped
				} else {
					f.addTab(sess, true)
				}
				if err := store.Save(sess); err != nil {
					t.Fatal(err)
				}
			}

			res, err := l.Open(Request{Name: "c", Resume: ResumeStored})
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			if len(res.Warnings) != 0 {
				t.Errorf("warnings = %v, want none", res.Warnings)
			}
			// c's tab comes after a's (2, 3) and b's (4, 5): sidebar 6, claude 7.
			want := slices.Concat(
				[]string{"Windows"}, // the resume snapshot
				newTabLaunch(6, 7, "c"),
				[]string{"Windows", "CloseTab(1)"}, // the home tab retires
				tc.wantExtra,
			)
			if !slices.Equal(f.calls, want) {
				t.Errorf("calls = %v, want %v", f.calls, want)
			}
			checkRanks(t, store, tc.wantRanks)
		})
	}
}
