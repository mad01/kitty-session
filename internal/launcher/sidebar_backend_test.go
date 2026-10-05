package launcher

import (
	"errors"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/mad01/kitty-session/internal/kitty"
	"github.com/mad01/kitty-session/internal/session"
	"github.com/mad01/kitty-session/internal/sidebar"
)

func itoa(n int) string { return strconv.Itoa(n) }

// countCalls returns how often the fake recorded name.
func countCalls(f *fakeKitty, name string) int {
	n := 0
	for _, c := range f.calls {
		if c == name {
			n++
		}
	}
	return n
}

// fakeStates stands in for the state directory: one entry per session name.
type fakeStates map[string]struct {
	state string
	at    time.Time
}

func (f fakeStates) read(name string) (string, time.Time, error) {
	e, ok := f[name]
	if !ok {
		return "", time.Time{}, errors.New("no state file")
	}
	return e.state, e.at, nil
}

// newTestBackend wires a home-tab backend (no own session) over the fake
// instance with a fixed clock and no state files. The clock stands still
// until the test moves it; a test that needs an own session sets b.ownID to
// its record id after creating it.
func newTestBackend(t *testing.T) (*SidebarBackend, *fakeKitty, *time.Time) {
	t.Helper()
	l, f, _ := newTestLauncher(t)
	clock := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return clock }
	b := newSidebarBackend(l, nil, "")
	b.readState = fakeStates{}.read
	return b, f, &clock
}

func TestResolveState(t *testing.T) {
	now := time.Now()
	fresh := now.Add(-time.Second)
	stale := now.Add(-time.Minute)
	viewed := now.Add(-10 * time.Minute)
	tests := []struct {
		name string
		in   stateInput
		want sidebar.State
	}{
		{"stopped record", stateInput{hasWindow: true, title: "◐ busy"}, sidebar.StateStopped},
		{"claude window gone", stateInput{active: true, title: "◐ busy"}, sidebar.StateStopped},
		{
			"fresh input beats a working title",
			stateInput{
				active:    true,
				hasWindow: true,
				title:     "◐ busy",
				fileState: "input",
				fileAt:    fresh,
			},
			sidebar.StateInput,
		},
		{
			"stale input loses to a working title",
			stateInput{
				active:    true,
				hasWindow: true,
				title:     "◐ busy",
				fileState: "input",
				fileAt:    stale,
			},
			sidebar.StateWorking,
		},
		{
			"braille spinner is working",
			stateInput{
				active:    true,
				hasWindow: true,
				title:     "⠋ Claude Code",
				fileState: "idle",
				fileAt:    fresh,
			},
			sidebar.StateWorking,
		},
		{
			"idle title, no state file",
			stateInput{active: true, hasWindow: true, title: "✳ Claude Code"},
			sidebar.StateIdle,
		},
		{
			"idle title, finished after last view",
			stateInput{
				active: true, hasWindow: true, title: "✳ Claude Code",
				fileState: "idle", fileAt: stale, viewedAt: viewed,
			},
			sidebar.StateDone,
		},
		{
			"idle title, finished before last view",
			stateInput{
				active: true, hasWindow: true, title: "✳ Claude Code",
				fileState: "idle", fileAt: viewed, viewedAt: stale,
			},
			sidebar.StateIdle,
		},
		{
			"idle title, never viewed, finished",
			stateInput{
				active:    true,
				hasWindow: true,
				title:     "✳ Claude Code",
				fileState: "idle",
				fileAt:    stale,
			},
			sidebar.StateDone,
		},
		{
			"fresh working state file outranks the idle glyph",
			stateInput{
				active:    true,
				hasWindow: true,
				title:     "✳ Claude Code",
				fileState: "working",
				fileAt:    fresh,
			},
			sidebar.StateWorking,
		},
		{
			"stale working state file loses to the idle glyph",
			stateInput{
				active:    true,
				hasWindow: true,
				title:     "✳ Claude Code",
				fileState: "working",
				fileAt:    stale,
				viewedAt:  viewed,
			},
			sidebar.StateIdle,
		},
		{
			"no glyph, state file working",
			stateInput{
				active:    true,
				hasWindow: true,
				title:     "Claude Code",
				fileState: "working",
				fileAt:    stale,
			},
			sidebar.StateWorking,
		},
		{
			"no glyph, stale input",
			stateInput{
				active:    true,
				hasWindow: true,
				title:     "Claude Code",
				fileState: "input",
				fileAt:    stale,
			},
			sidebar.StateInput,
		},
		{
			"no glyph, state file idle is idle not done",
			stateInput{
				active:    true,
				hasWindow: true,
				title:     "Claude Code",
				fileState: "idle",
				fileAt:    stale,
			},
			sidebar.StateIdle,
		},
		{
			"no glyph, waiting",
			stateInput{
				active:    true,
				hasWindow: true,
				title:     "~/code",
				fileState: "waiting",
				fileAt:    fresh,
			},
			sidebar.StateIdle,
		},
		{
			"no glyph, nothing",
			stateInput{active: true, hasWindow: true, title: ""},
			sidebar.StateIdle,
		},
		{
			"unknown state file value",
			stateInput{
				active:    true,
				hasWindow: true,
				title:     "zsh",
				fileState: "bogus",
				fileAt:    fresh,
			},
			sidebar.StateIdle,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveState(tt.in); got != tt.want {
				t.Errorf("resolveState = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestListMatchesWindowsByTagAndFillsRows(t *testing.T) {
	b, f, _ := newTestBackend(t)
	t0 := time.Date(2026, 10, 5, 11, 0, 0, 0, time.UTC)
	states := fakeStates{}
	b.readState = states.read

	live := session.New("live", "/work/live", 0, 0)
	f.addTab(live, true)
	f.find(live.KittyWindowID).Title = "✳ Plan the merge"
	states["live"] = struct {
		state string
		at    time.Time
	}{"idle", t0}

	// Same window ids as live, but tagged with another id: the stored ids
	// alone must not make this session alive.
	stale := session.New("stale", "/work/stale", live.KittyTabID, live.KittyWindowID)
	stale.KittySidebarWindowID = live.KittySidebarWindowID

	stopped := session.New("stopped", "/work/stopped", 0, 0)
	stopped.Status = session.StatusStopped
	f.addTab(stopped, true)

	home := t.TempDir()
	t.Setenv("HOME", home)
	b.home = home
	inHome := session.New("inhome", home+"/code/x", 0, 0)
	f.addTab(inHome, true) // no title on the fake claude window
	f.find(inHome.KittyWindowID).Title = ""

	for _, s := range []*session.Session{live, stale, stopped, inHome} {
		if err := b.l.store.Save(s); err != nil {
			t.Fatal(err)
		}
	}

	agents, err := b.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	byName := map[string]sidebar.Agent{}
	for _, a := range agents {
		byName[a.Name] = a
	}
	if len(byName) != 4 {
		t.Fatalf("got %d agents, want 4: %+v", len(byName), agents)
	}
	if a := byName["live"]; a.State != sidebar.StateDone || a.Title != "Plan the merge" ||
		!a.ChangedAt.Equal(t0) || a.Dir != "/work/live" {
		t.Errorf("live = %+v", a)
	}
	if a := byName["stale"]; a.State != sidebar.StateStopped || !a.ChangedAt.IsZero() {
		t.Errorf("stale = %+v, want stopped with zero ChangedAt", a)
	}
	if a := byName["stopped"]; a.State != sidebar.StateStopped {
		t.Errorf("stopped = %+v", a)
	}
	if a := byName["inhome"]; a.Title != "~/code/x" || a.State != sidebar.StateIdle {
		t.Errorf("inhome = %+v, want ~ title and idle", a)
	}
	if n := countCalls(f, "Windows"); n != 1 {
		t.Errorf("List took %d snapshots, want 1: %v", n, f.calls)
	}
}

func TestListStampsViewedAtAndDropsDoneToIdle(t *testing.T) {
	b, f, clock := newTestBackend(t)
	finished := clock.Add(-time.Minute)
	states := fakeStates{"own": {"idle", finished}}
	b.readState = states.read

	own := session.New("own", "/work/own", 0, 0)
	b.ownID = own.ID
	f.addTab(own, true)
	f.find(own.KittyWindowID).Title = "✳ Claude Code"
	if err := b.l.store.Save(own); err != nil {
		t.Fatal(err)
	}
	other := session.New("other", "/work/other", 0, 0)
	f.addTab(other, true)
	if err := b.l.store.Save(other); err != nil {
		t.Fatal(err)
	}

	state := func() (sidebar.State, bool) {
		t.Helper()
		agents, err := b.List()
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		for _, a := range agents {
			if a.Name == "own" {
				return a.State, a.Own
			}
		}
		t.Fatal("own missing from List")
		return 0, false
	}
	viewedAt := func() time.Time {
		t.Helper()
		sess, err := b.l.store.Load("own")
		if err != nil {
			t.Fatal(err)
		}
		return sess.ViewedAt
	}

	// Tab in the background: finished after the (never) view → done, no stamp.
	if got, isOwn := state(); got != sidebar.StateDone || !isOwn {
		t.Errorf("background: state %s own %v, want done and own", got, isOwn)
	}
	if !viewedAt().IsZero() {
		t.Error("ViewedAt stamped while the tab was not active")
	}

	// Tab comes to the front: stamped now, so the finish is older → idle.
	for i := range f.windows {
		if f.windows[i].TabID == own.KittyTabID {
			f.windows[i].TabActive = true
		}
	}
	if got, _ := state(); got != sidebar.StateIdle {
		t.Errorf("active tab: state %s, want idle", got)
	}
	first := viewedAt()
	if !first.Equal(*clock) {
		t.Errorf("ViewedAt = %v, want %v", first, *clock)
	}

	// Within the debounce the record is left alone.
	*clock = clock.Add(viewedDebounce - time.Second)
	state()
	if got := viewedAt(); !got.Equal(first) {
		t.Errorf("ViewedAt re-stamped inside the debounce: %v", got)
	}

	// Past it, a new stamp. A new finish in between shows as done first.
	*clock = clock.Add(2 * time.Second)
	states["own"] = struct {
		state string
		at    time.Time
	}{"idle", clock.Add(-time.Millisecond)}
	if got, _ := state(); got != sidebar.StateIdle {
		t.Errorf("after re-stamp: state %s, want idle", got)
	}
	if got := viewedAt(); !got.Equal(*clock) {
		t.Errorf("ViewedAt = %v, want %v", got, *clock)
	}

	// The other session is never stamped by this sidebar.
	sess, err := b.l.store.Load("other")
	if err != nil {
		t.Fatal(err)
	}
	if !sess.ViewedAt.IsZero() {
		t.Error("other session stamped by own's sidebar")
	}
}

func TestPinWidth(t *testing.T) {
	b, f, _ := newTestBackend(t)
	own := session.New("own", "/work/own", 0, 0)
	b.ownID = own.ID
	f.addTab(own, true)
	if err := b.l.store.Save(own); err != nil {
		t.Fatal(err)
	}
	f.find(own.KittySidebarWindowID).Columns = 40

	if err := b.PinWidth(36); err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(f.calls, func(c string) bool { return c != "" && c[0] == 'R' }) {
		t.Errorf("resized although the sidebar reports the configured width: %v", f.calls)
	}
	if err := b.PinWidth(40); err != nil {
		t.Fatal(err)
	}
	want := "ResizeWindow(" + itoa(own.KittySidebarWindowID) + ",horizontal,-4)"
	if !slices.Contains(f.calls, want) {
		t.Errorf("calls = %v, want %s", f.calls, want)
	}

	home, fh, _ := newTestBackend(t)
	if err := home.PinWidth(80); err != nil || len(fh.calls) != 0 {
		t.Errorf("home tab: err %v, calls %v; want nothing", err, fh.calls)
	}
}

func TestShellSplitAndFocusAgentWindow(t *testing.T) {
	b, f, _ := newTestBackend(t)
	own := session.New("own", "/work/own", 0, 0)
	b.ownID = own.ID
	f.addTab(own, true)
	if err := b.l.store.Save(own); err != nil {
		t.Fatal(err)
	}

	if err := b.ShellSplit(); err != nil {
		t.Fatalf("ShellSplit: %v", err)
	}
	want := "LaunchHSplit(" + itoa(own.KittyWindowID) + ",bias=30)"
	if !slices.Contains(f.calls, want) {
		t.Errorf("calls = %v, want %s", f.calls, want)
	}
	shell := f.launches[len(f.launches)-1]
	if shell.Dir != "/work/own" || !slices.Contains(shell.Vars, "KS_SESSION_ID="+own.ID) {
		t.Errorf("shell launch = %+v", shell)
	}

	if err := b.FocusAgentWindow(); err != nil {
		t.Fatalf("FocusAgentWindow: %v", err)
	}
	if want := "FocusWindow(" + itoa(own.KittyWindowID) + ")"; !slices.Contains(f.calls, want) {
		t.Errorf("calls = %v, want %s", f.calls, want)
	}
	sess, err := b.l.store.Load("own")
	if err != nil {
		t.Fatal(err)
	}
	if sess.FocusedAt.IsZero() {
		t.Error("FocusAgentWindow did not stamp FocusedAt")
	}

	// Without a claude window both report it rather than acting elsewhere.
	f.windows = slices.DeleteFunc(f.windows, func(w kitty.Window) bool {
		return w.ID == own.KittyWindowID
	})
	if err := b.ShellSplit(); err == nil {
		t.Error("ShellSplit without a claude window returned nil")
	}
	if err := b.FocusAgentWindow(); err == nil {
		t.Error("FocusAgentWindow without a claude window returned nil")
	}

	home, fh, _ := newTestBackend(t)
	if err := home.FocusAgentWindow(); err != nil || len(fh.calls) != 0 {
		t.Errorf("home tab FocusAgentWindow: err %v, calls %v", err, fh.calls)
	}
	if err := home.ShellSplit(); err == nil {
		t.Error("home tab ShellSplit returned nil")
	}
}

func TestSessionActions(t *testing.T) {
	b, f, _ := newTestBackend(t)

	if err := b.New("", "/work/Fresh Dir"); err != nil {
		t.Fatalf("New: %v", err)
	}
	if !b.l.store.Exists("fresh-dir") {
		t.Error("New without a name did not derive one from the directory")
	}
	if err := b.New("fresh-dir", "/work/x"); !errors.Is(err, ErrExists) {
		t.Errorf("New with a taken name: %v, want ErrExists", err)
	}
	if err := b.New("", ""); err == nil {
		t.Error("New with nothing to name it from returned nil")
	}

	if err := b.Rename("fresh-dir", "renamed"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if err := b.Close("renamed", true); err != nil {
		t.Fatalf("Close keep: %v", err)
	}
	sess, err := b.l.store.Load("renamed")
	if err != nil {
		t.Fatal(err)
	}
	if sess.IsActive() {
		t.Error("Close keep left the record active")
	}
	if err := b.Close("renamed", false); err != nil {
		t.Fatalf("Close delete: %v", err)
	}
	names, err := b.Trashed()
	if err != nil || !slices.Equal(names, []string{"renamed"}) {
		t.Errorf("Trashed = %v, %v; want [renamed]", names, err)
	}
	if err := b.Restore("renamed"); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if !b.l.store.Exists("renamed") {
		t.Error("Restore did not bring the record back")
	}
	if err := b.Close("missing", true); err == nil {
		t.Error("Close of an unknown session returned nil")
	}

	quit := false
	b.quit = func() error { quit = true; return nil }
	if err := b.Quit(); err != nil || !quit {
		t.Errorf("Quit: err %v, called %v", err, quit)
	}
	if _, err := b.Repos(); err == nil {
		t.Error("Repos without config returned nil")
	}
	_ = f
}

func TestHooksSummary(t *testing.T) {
	all := []string{"PreToolUse", "Stop", "Notification", "SessionStart", "SessionEnd"}
	tests := []struct {
		installed []string
		want      string
	}{
		{nil, "hooks: none registered (ks hooks install)"},
		{all, "hooks: all 5 events registered"},
		{[]string{"PreToolUse", "Notification"}, "hooks: missing Stop, SessionStart, SessionEnd"},
	}
	for _, tt := range tests {
		if got := hooksSummary(tt.installed); got != tt.want {
			t.Errorf("hooksSummary(%v) = %q, want %q", tt.installed, got, tt.want)
		}
	}
}

// TestListReflectsRenameByID proves the own session is tracked by id, so a
// rename shows the new name and keeps the own marker without restarting the
// sidebar.
func TestListReflectsRenameByID(t *testing.T) {
	b, f, _ := newTestBackend(t)
	own := session.New("before", "/work/own", 0, 0)
	b.ownID = own.ID
	f.addTab(own, true)
	if err := b.l.store.Save(own); err != nil {
		t.Fatal(err)
	}

	ownRow := func() (sidebar.Agent, bool) {
		t.Helper()
		agents, err := b.List()
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		for _, a := range agents {
			if a.Own {
				return a, true
			}
		}
		return sidebar.Agent{}, false
	}

	a, ok := ownRow()
	if !ok || a.Name != "before" {
		t.Fatalf("own row = %+v, ok %v; want name before", a, ok)
	}
	if err := b.Rename("before", "after"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	a, ok = ownRow()
	if !ok || a.Name != "after" {
		t.Errorf("after rename own row = %+v, ok %v; want name after and still own", a, ok)
	}
}
