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
	"github.com/mad01/kitty-session/internal/state"
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

// fakeStates stands in for the state directory: one entry per session name,
// every one written by a hook (no source).
type fakeStates map[string]struct {
	state string
	at    time.Time
}

func (f fakeStates) read(name string) (state.Entry, error) {
	e, ok := f[name]
	if !ok {
		return state.Entry{}, errors.New("no state file")
	}
	return state.Entry{State: e.state, UpdatedAt: e.at}, nil
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
	hoursOld := now.Add(-3 * time.Hour)
	blocked, working, done := kitty.ProgramBlocked, kitty.ProgramWorking, kitty.ProgramDone
	failed, idle := kitty.ProgramError, kitty.ProgramIdle
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
			"idle title, finished after last view, on the visible tab is idle",
			stateInput{
				active: true, hasWindow: true, title: "✳ Claude Code",
				fileState: "idle", fileAt: stale, viewedAt: viewed, visible: true,
			},
			sidebar.StateIdle,
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
			"input hours old stays input under the idle glyph",
			stateInput{
				active: true, hasWindow: true, title: "✳ Claude Code",
				fileState: "input", fileAt: hoursOld, viewedAt: viewed,
			},
			sidebar.StateInput,
		},
		{
			"input hours old loses to a working title",
			stateInput{
				active:    true,
				hasWindow: true,
				title:     "◐ busy",
				fileState: "input",
				fileAt:    hoursOld,
			},
			sidebar.StateWorking,
		},
		{
			"input hours old, no title",
			stateInput{active: true, hasWindow: true, fileState: "input", fileAt: hoursOld},
			sidebar.StateInput,
		},
		{
			"fresh error beats a working title",
			stateInput{
				active:    true,
				hasWindow: true,
				title:     "◐ busy",
				fileState: "error",
				fileAt:    fresh,
			},
			sidebar.StateError,
		},
		{
			"stale error stays error under the idle glyph",
			stateInput{
				active: true, hasWindow: true, title: "✳ Claude Code",
				fileState: "error", fileAt: stale, viewedAt: viewed,
			},
			sidebar.StateError,
		},
		{
			"stale error loses to a working title",
			stateInput{
				active:    true,
				hasWindow: true,
				title:     "◐ busy",
				fileState: "error",
				fileAt:    stale,
			},
			sidebar.StateWorking,
		},
		{
			"stale error, no glyph",
			stateInput{
				active:    true,
				hasWindow: true,
				title:     "Claude Code",
				fileState: "error",
				fileAt:    stale,
			},
			sidebar.StateError,
		},
		{
			"pi: stopped record",
			stateInput{kind: session.AgentPi, hasWindow: true, fileState: "working", fileAt: fresh},
			sidebar.StateStopped,
		},
		{
			"pi: window gone",
			stateInput{kind: session.AgentPi, active: true, fileState: "working", fileAt: fresh},
			sidebar.StateStopped,
		},
		{
			"pi: fresh input",
			stateInput{
				kind: session.AgentPi, active: true, hasWindow: true,
				fileState: "input", fileAt: fresh,
			},
			sidebar.StateInput,
		},
		{
			"pi: fresh working",
			stateInput{
				kind: session.AgentPi, active: true, hasWindow: true,
				fileState: "working", fileAt: fresh,
			},
			sidebar.StateWorking,
		},
		{
			"pi: input hours old stays input",
			stateInput{
				kind: session.AgentPi, active: true, hasWindow: true,
				fileState: "input", fileAt: hoursOld,
			},
			sidebar.StateInput,
		},
		{
			"pi: working hours old stays working",
			stateInput{
				kind: session.AgentPi, active: true, hasWindow: true,
				fileState: "working", fileAt: hoursOld,
			},
			sidebar.StateWorking,
		},
		{
			"pi: idle after last view is done",
			stateInput{
				kind: session.AgentPi, active: true, hasWindow: true,
				fileState: "idle", fileAt: stale, viewedAt: viewed,
			},
			sidebar.StateDone,
		},
		{
			"pi: idle after last view on the visible tab is idle",
			stateInput{
				kind: session.AgentPi, active: true, hasWindow: true,
				fileState: "idle", fileAt: stale, viewedAt: viewed, visible: true,
			},
			sidebar.StateIdle,
		},
		{
			"pi: idle before last view is idle",
			stateInput{
				kind: session.AgentPi, active: true, hasWindow: true,
				fileState: "idle", fileAt: viewed, viewedAt: stale,
			},
			sidebar.StateIdle,
		},
		{
			"pi: a working title glyph means nothing",
			stateInput{kind: session.AgentPi, active: true, hasWindow: true, title: "◐ busy"},
			sidebar.StateIdle,
		},
		{
			"pi: waiting is idle",
			stateInput{
				kind: session.AgentPi, active: true, hasWindow: true,
				fileState: "waiting", fileAt: fresh,
			},
			sidebar.StateIdle,
		},
		{
			"pi: no state file",
			stateInput{kind: session.AgentPi, active: true, hasWindow: true},
			sidebar.StateIdle,
		},
		{
			"shell: stopped record",
			stateInput{kind: session.AgentShell, hasWindow: true},
			sidebar.StateStopped,
		},
		{
			"shell: window gone",
			stateInput{kind: session.AgentShell, active: true},
			sidebar.StateStopped,
		},
		{
			"shell: alive is idle whatever the title",
			stateInput{kind: session.AgentShell, active: true, hasWindow: true, title: "◐ make"},
			sidebar.StateIdle,
		},
		{
			"shell: a stray state file is ignored",
			stateInput{
				kind: session.AgentShell, active: true, hasWindow: true,
				fileState: "input", fileAt: fresh,
			},
			sidebar.StateIdle,
		},
		{
			"stopped record with an input file",
			stateInput{
				hasWindow: true,
				title:     "✳ Claude Code",
				fileState: "input",
				fileAt:    hoursOld,
			},
			sidebar.StateStopped,
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
		{
			"record: blocked is input under the idle glyph",
			stateInput{active: true, hasWindow: true, title: "✳ Claude Code", report: blocked},
			sidebar.StateInput,
		},
		{
			"record: working under the idle glyph",
			stateInput{active: true, hasWindow: true, title: "✳ Claude Code", report: working},
			sidebar.StateWorking,
		},
		{
			"record: done when the state file was seen",
			stateInput{
				active: true, hasWindow: true, title: "✳ Claude Code",
				fileState: "idle", fileAt: viewed, viewedAt: stale, report: done,
			},
			sidebar.StateDone,
		},
		{
			"record: done on the active tab is idle",
			stateInput{
				active: true, hasWindow: true, title: "✳ Claude Code",
				report: done, visible: true,
			},
			sidebar.StateIdle,
		},
		{
			"record: error",
			stateInput{active: true, hasWindow: true, title: "✳ Claude Code", report: failed},
			sidebar.StateError,
		},
		{
			"record: idle under a working glyph",
			stateInput{active: true, hasWindow: true, title: "◐ busy", report: idle},
			sidebar.StateIdle,
		},
		{
			"record: a fresh working state file outranks a blocked record",
			stateInput{
				active: true, hasWindow: true, title: "✳ Claude Code",
				fileState: "working", fileAt: fresh, report: blocked,
			},
			sidebar.StateWorking,
		},
		{
			"record: blocked outranks a stale working state file",
			stateInput{
				active: true, hasWindow: true, title: "◐ busy",
				fileState: "working", fileAt: stale, report: blocked,
			},
			sidebar.StateInput,
		},
		{
			"record: pi blocked over a stale idle state file",
			stateInput{
				kind: session.AgentPi, active: true, hasWindow: true,
				fileState: "idle", fileAt: viewed, viewedAt: stale, report: blocked,
			},
			sidebar.StateInput,
		},
		{
			"record: pi fresh working state file outranks a blocked record",
			stateInput{
				kind: session.AgentPi, active: true, hasWindow: true,
				fileState: "working", fileAt: fresh, report: blocked,
			},
			sidebar.StateWorking,
		},
		{
			"record: shell working",
			stateInput{kind: session.AgentShell, active: true, hasWindow: true, report: working},
			sidebar.StateWorking,
		},
		{
			"record: shell with a fresh state file is idle, its own rule",
			stateInput{
				kind: session.AgentShell, active: true, hasWindow: true,
				fileState: "input", fileAt: fresh, report: working,
			},
			sidebar.StateIdle,
		},
		{
			"record: unknown state falls through to the kind's rules",
			stateInput{
				active: true, hasWindow: true, title: "✳ Claude Code",
				report: kitty.ProgramState("paused"),
			},
			sidebar.StateIdle,
		},
		{
			"record: stopped record stays stopped",
			stateInput{hasWindow: true, title: "✳ Claude Code", report: blocked},
			sidebar.StateStopped,
		},
		{
			"record: window gone stays stopped",
			stateInput{active: true, report: blocked},
			sidebar.StateStopped,
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
		a.Tab != 2 || a.Dir != "/work/live" {
		t.Errorf("live = %+v", a)
	}
	if a := byName["stale"]; a.State != sidebar.StateStopped || a.Tab != 0 {
		t.Errorf("stale = %+v, want stopped with no tab", a)
	}
	if a := byName["stopped"]; a.State != sidebar.StateStopped || a.Tab != 3 {
		t.Errorf("stopped = %+v", a)
	}
	if a := byName["inhome"]; a.Title != "~/code/x" || a.State != sidebar.StateIdle {
		t.Errorf("inhome = %+v, want ~ title and idle", a)
	}
	if n := countCalls(f, "Windows"); n != 1 {
		t.Errorf("List took %d snapshots, want 1: %v", n, f.calls)
	}
}

// TestListReadsTheProgramStatus checks that a row follows its window's
// Program Status record over the title and a stale state file, that only a
// state file saying input gives an input row its waiting time, that done on
// the active tab reads idle, and that a window without a record keeps the
// old rules.
func TestListReadsTheProgramStatus(t *testing.T) {
	b, f, _ := newTestBackend(t)
	states := fakeStates{}
	b.readState = states.read
	now := b.l.now()
	type entry = struct {
		state string
		at    time.Time
	}

	asked := session.New("asked", "/work/asked", 0, 0) // blocked over a stale working file
	f.addTab(asked, true)
	f.find(asked.KittyWindowID).Status = kitty.ProgramBlocked
	states["asked"] = entry{"working", now.Add(-time.Minute)}

	seen := session.New("seen", "/work/seen", 0, 0) // blocked over an old idle file
	f.addTab(seen, true)
	f.find(seen.KittyWindowID).Status = kitty.ProgramBlocked
	states["seen"] = entry{"idle", now.Add(-time.Hour)}

	prompt := session.New("prompt", "/work/prompt", 0, 0) // blocked, and the file says so
	f.addTab(prompt, true)
	f.find(prompt.KittyWindowID).Status = kitty.ProgramBlocked
	states["prompt"] = entry{"input", now.Add(-time.Hour)}

	failed := session.New("failed", "/work/failed", 0, 0)
	failed.Agent = session.AgentShell
	f.addTab(failed, true)
	f.find(failed.KittyWindowID).Status = kitty.ProgramError

	shown := session.New("shown", "/work/shown", 0, 0) // done on the active tab
	f.addTab(shown, true)
	f.find(shown.KittyWindowID).Status = kitty.ProgramDone
	f.find(shown.KittyWindowID).TabActive = true

	plain := session.New("plain", "/work/plain", 0, 0) // no record: the ✳ title rules
	f.addTab(plain, true)

	for _, s := range []*session.Session{asked, seen, prompt, failed, shown, plain} {
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
	want := map[string]sidebar.Agent{
		"asked":  {State: sidebar.StateInput},
		"seen":   {State: sidebar.StateInput},
		"prompt": {State: sidebar.StateInput, Waiting: time.Hour},
		"failed": {State: sidebar.StateError},
		"shown":  {State: sidebar.StateIdle},
		"plain":  {State: sidebar.StateIdle},
	}
	for name, w := range want {
		if a := byName[name]; a.State != w.State || a.Waiting != w.Waiting {
			t.Errorf("%s = %s waiting %s, want %s waiting %s",
				name, a.State, a.Waiting, w.State, w.Waiting)
		}
	}
}

// TestListReportsKind checks that each row carries its record's kind and
// that the kind picks the state rules: a pi row follows its state file with
// no title to read, a shell row is idle.
func TestListReportsKind(t *testing.T) {
	b, f, _ := newTestBackend(t)
	states := fakeStates{}
	b.readState = states.read

	pi := session.New("pi-one", "/work/pi", 0, 0)
	pi.Agent = session.AgentPi
	f.addTab(pi, true)
	f.find(pi.KittyWindowID).Title = "pi"
	states["pi-one"] = struct {
		state string
		at    time.Time
	}{"input", b.l.now().Add(-time.Hour)}

	sh := session.New("sh-one", "/work/sh", 0, 0)
	sh.Agent = session.AgentShell
	f.addTab(sh, true)
	f.find(sh.KittyWindowID).Title = "◐ make"

	old := session.New("old", "/work/old", 0, 0) // a record from before the field
	f.addTab(old, true)

	for _, s := range []*session.Session{pi, sh, old} {
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
	if a := byName["pi-one"]; a.Kind != session.AgentPi || a.State != sidebar.StateInput {
		t.Errorf("pi row = %+v, want kind pi at input", a)
	}
	if a := byName["sh-one"]; a.Kind != session.AgentShell || a.State != sidebar.StateIdle {
		t.Errorf("shell row = %+v, want kind shell at idle", a)
	}
	if a := byName["old"]; a.Kind != session.AgentClaude {
		t.Errorf("old row kind = %q, want claude", a.Kind)
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

	// Tab comes to the front: stamped at once, and a visible tab never reads
	// done, so the finish is older → idle.
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

func TestUnread(t *testing.T) {
	viewed := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	after, before := viewed.Add(time.Second), viewed.Add(-time.Second)
	tests := []struct {
		name string
		in   stateInput
		st   sidebar.State
		want bool
	}{
		{
			"file newer than the view on a background tab",
			stateInput{fileAt: after, viewedAt: viewed},
			sidebar.StateWorking, true,
		},
		{"never viewed", stateInput{fileAt: viewed}, sidebar.StateIdle, true},
		{
			"while the tab is visible",
			stateInput{fileAt: after, viewedAt: viewed, visible: true},
			sidebar.StateWorking,
			false,
		},
		{
			"a waiting file is a launch, not activity",
			stateInput{fileState: "waiting", fileAt: after, viewedAt: viewed},
			sidebar.StateIdle,
			false,
		},
		{
			"file older than the view",
			stateInput{fileAt: before, viewedAt: viewed},
			sidebar.StateIdle, false,
		},
		{"no state file", stateInput{viewedAt: viewed}, sidebar.StateIdle, false},
		{"no state file and never viewed", stateInput{}, sidebar.StateIdle, false},
		{
			"stopped", stateInput{fileAt: after, viewedAt: viewed}, sidebar.StateStopped, false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := unread(tc.in, tc.st); got != tc.want {
				t.Errorf("unread = %v, want %v", got, tc.want)
			}
		})
	}
}

// ownFixture is a backend sitting in its own session's tab, the tab in the
// background to start with, over a working state file written on the
// backend's clock. The clock starts at wall-clock time because state
// freshness is measured against it, and the file's age is zero so no run,
// however slow, can turn it stale and hand the row to the ✳ title.
type ownFixture struct {
	t      *testing.T
	b      *SidebarBackend
	f      *fakeKitty
	clock  *time.Time
	own    *session.Session
	states fakeStates
}

func newOwnFixture(t *testing.T) *ownFixture {
	t.Helper()
	b, f, clock := newTestBackend(t)
	*clock = time.Now()
	states := fakeStates{"own": {"working", *clock}}
	b.readState = states.read
	own := session.New("own", "/work/own", 0, 0)
	b.ownID = own.ID
	f.addTab(own, true)
	if err := b.l.store.Save(own); err != nil {
		t.Fatal(err)
	}
	return &ownFixture{t: t, b: b, f: f, clock: clock, own: own, states: states}
}

// show puts the own tab in front of the OS window or behind another tab,
// and sets whether the OS window has keyboard focus.
func (x *ownFixture) show(tabActive, osFocused bool) {
	for i := range x.f.windows {
		if x.f.windows[i].TabID == x.own.KittyTabID {
			x.f.windows[i].TabActive = tabActive
		}
	}
	x.f.osUnfocused = !osFocused
}

// advance moves the backend's clock.
func (x *ownFixture) advance(d time.Duration) { *x.clock = x.clock.Add(d) }

// setFile writes the own session's state file.
func (x *ownFixture) setFile(st string, at time.Time) {
	x.states["own"] = struct {
		state string
		at    time.Time
	}{st, at}
}

// poll runs one List and returns the own row.
func (x *ownFixture) poll() sidebar.Agent {
	x.t.Helper()
	agents, err := x.b.List()
	if err != nil {
		x.t.Fatalf("List: %v", err)
	}
	for _, a := range agents {
		if a.Name == "own" {
			return a
		}
	}
	x.t.Fatal("own missing from List")
	return sidebar.Agent{}
}

// wantRow polls and checks the own row's unread mark and state.
func (x *ownFixture) wantRow(unread bool, st sidebar.State) {
	x.t.Helper()
	if a := x.poll(); a.Unread != unread || a.State != st {
		x.t.Errorf("own row: unread %v state %s, want %v and %s", a.Unread, a.State, unread, st)
	}
}

// wantViewedAt checks the own record's stamp; why names the rule under test.
func (x *ownFixture) wantViewedAt(want time.Time, why string) {
	x.t.Helper()
	sess, err := x.b.l.store.Load("own")
	if err != nil {
		x.t.Fatal(err)
	}
	if !sess.ViewedAt.Equal(want) {
		x.t.Errorf("ViewedAt = %v, want %v: %s", sess.ViewedAt, want, why)
	}
}

// setMoving stamps or clears the KS_MOVING marker on the own sidebar window,
// as ks move does around a reorder.
func (x *ownFixture) setMoving(at time.Time) {
	x.f.find(x.own.KittySidebarWindowID).MovingAt = at
}

// TestListStampsViewedAtOnVisibility covers when the own record is stamped
// as viewed. Visible means the tab is in front of a focused OS window with
// no fresh ks move marker on it.
func TestListStampsViewedAtOnVisibility(t *testing.T) {
	t.Run("in the background the row is unread and nothing is stamped", func(t *testing.T) {
		x := newOwnFixture(t)
		x.wantRow(true, sidebar.StateWorking)
		x.wantViewedAt(time.Time{}, "stamped while the tab was in the background")
	})
	t.Run("newly visible is stamped on that poll", func(t *testing.T) {
		x := newOwnFixture(t)
		x.poll()
		x.show(true, true)
		x.wantRow(false, sidebar.StateWorking)
		x.wantViewedAt(*x.clock, "not stamped when the tab came to the front")
	})
	t.Run("a fresh move marker keeps the tab from counting as seen", func(t *testing.T) {
		x := newOwnFixture(t)
		x.poll()
		x.show(true, true)
		x.setMoving(*x.clock) // ks move has the tab up to reorder it
		x.wantRow(true, sidebar.StateWorking)
		x.wantViewedAt(time.Time{}, "stamped while the move marker was fresh")
		x.setMoving(time.Time{}) // the move cleared it
		x.wantRow(false, sidebar.StateWorking)
		x.wantViewedAt(*x.clock, "not stamped once the move marker was cleared")
	})
	t.Run("staying visible is stamped once per debounce", func(t *testing.T) {
		x := newOwnFixture(t)
		x.show(true, true)
		x.poll()
		first := *x.clock
		x.advance(time.Second)
		x.poll()
		x.wantViewedAt(first, "re-stamped inside the debounce")
		x.advance(viewedDebounce)
		x.poll()
		x.wantViewedAt(*x.clock, "not re-stamped past the debounce")
	})
	t.Run("leaving is stamped once more", func(t *testing.T) {
		x := newOwnFixture(t)
		x.show(true, true)
		x.poll()
		x.advance(time.Second)
		x.show(false, true)
		x.poll()
		x.wantViewedAt(*x.clock, "not stamped when the tab left the front")
		left := *x.clock
		x.advance(time.Second)
		x.poll()
		x.wantViewedAt(left, "stamped again while still in the background")
	})
	t.Run("a tab in front of an unfocused OS window is not visible", func(t *testing.T) {
		x := newOwnFixture(t)
		x.show(true, false)
		x.wantRow(true, sidebar.StateWorking)
		x.wantViewedAt(time.Time{}, "stamped while the OS window was unfocused")
	})
	t.Run("a done record stays done until the OS window is focused", func(t *testing.T) {
		x := newOwnFixture(t)
		x.f.find(x.own.KittyWindowID).Status = kitty.ProgramDone
		x.setFile("idle", x.clock.Add(-time.Minute)) // stale, so the record speaks
		x.show(true, false)
		x.wantRow(true, sidebar.StateDone)
		x.show(true, true)
		x.wantRow(false, sidebar.StateIdle)
		x.wantViewedAt(*x.clock, "not stamped once the OS window was focused")
	})
	t.Run("visible again inside the debounce is stamped at once", func(t *testing.T) {
		x := newOwnFixture(t)
		x.show(true, true)
		x.poll()
		x.show(false, true)
		x.advance(time.Second)
		x.poll() // the leaving stamp
		x.advance(time.Second)
		x.setFile("idle", *x.clock) // the turn ends in the background
		x.wantRow(true, sidebar.StateDone)
		x.show(true, true)
		x.advance(time.Second)
		x.wantRow(false, sidebar.StateIdle)
		x.wantViewedAt(*x.clock, "not stamped on return inside the debounce")
	})
}

func TestLiveVisible(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	win := func(tabActive, osFocused bool) *kitty.Window {
		return &kitty.Window{TabActive: tabActive, OSFocused: osFocused}
	}
	moving := func(tabActive, osFocused bool, at time.Time) *kitty.Window {
		w := win(tabActive, osFocused)
		w.MovingAt = at
		return w
	}
	tests := []struct {
		name string
		lv   live
		want bool
	}{
		{"tab in front of a focused OS window", live{claude: win(true, true)}, true},
		{"sidebar alone counts", live{sidebar: win(true, true)}, true},
		{"tab in front, OS window unfocused", live{claude: win(true, false)}, false},
		{"OS window focused, tab behind", live{claude: win(false, true)}, false},
		{"neither", live{claude: win(false, false)}, false},
		{"no windows", live{}, false},
		{
			"a fresh move marker hides a tab in front",
			live{sidebar: moving(true, true, now.Add(-time.Second))},
			false,
		},
		{
			"a stale move marker no longer hides it",
			live{sidebar: moving(true, true, now.Add(-2*movingTTL))},
			true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.lv.visible(now); got != tc.want {
				t.Errorf("visible = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestFocusStampsViewedAt checks that focusing a session from any sidebar
// marks it seen at once, so its unread mark and done state clear on every
// sidebar's next poll instead of waiting for the session's own sidebar.
func TestFocusStampsViewedAt(t *testing.T) {
	b, f, clock := newTestBackend(t)
	other := session.New("other", "/work/other", 0, 0)
	f.addTab(other, true)
	if err := b.l.store.Save(other); err != nil {
		t.Fatal(err)
	}
	if err := b.Focus("other"); err != nil {
		t.Fatalf("Focus: %v", err)
	}
	sess, err := b.l.store.Load("other")
	if err != nil {
		t.Fatal(err)
	}
	if !sess.ViewedAt.Equal(*clock) || !sess.FocusedAt.Equal(*clock) {
		t.Errorf("after Focus: ViewedAt %v FocusedAt %v, want both %v",
			sess.ViewedAt, sess.FocusedAt, *clock)
	}
}

func TestPinWidth(t *testing.T) {
	const sidebarCols = 40 // what the sidebar reports after a terminal resize
	tests := []struct {
		name       string
		withClaude bool
		setup      func(f *fakeKitty, own *session.Session)
		wantResize bool
	}{
		{
			name:       "sidebar window gone",
			withClaude: true,
			setup: func(f *fakeKitty, own *session.Session) {
				f.windows = slices.DeleteFunc(f.windows, func(w kitty.Window) bool {
					return w.ID == own.KittySidebarWindowID
				})
			},
		},
		{
			name:       "sidebar alone in its tab",
			withClaude: false,
			setup: func(f *fakeKitty, own *session.Session) {
				f.find(own.KittySidebarWindowID).Columns = sidebarCols
			},
		},
		{
			name:       "already the configured width",
			withClaude: true,
			setup: func(f *fakeKitty, own *session.Session) {
				f.find(own.KittySidebarWindowID).Columns = 36
			},
		},
		{
			name:       "resizes by the delta",
			withClaude: true,
			setup: func(f *fakeKitty, own *session.Session) {
				f.find(own.KittySidebarWindowID).Columns = sidebarCols
			},
			wantResize: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, f, _ := newTestBackend(t)
			own := session.New("own", "/work/own", 0, 0)
			b.ownID = own.ID
			f.addTab(own, tt.withClaude)
			if err := b.l.store.Save(own); err != nil {
				t.Fatal(err)
			}
			tt.setup(f, own)
			f.calls = nil

			if err := b.PinWidth(sidebarCols); err != nil {
				t.Fatal(err)
			}
			if n := countCalls(f, "Windows"); n != 1 {
				t.Errorf("took %d snapshots, want 1", n)
			}
			resize := "ResizeWindow(" + itoa(own.KittySidebarWindowID) + ",horizontal,-4)"
			if got := slices.Contains(f.calls, resize); got != tt.wantResize {
				t.Errorf("resize called %v, want %v: calls %v", got, tt.wantResize, f.calls)
			}
		})
	}

	t.Run("reported width equals the configured one", func(t *testing.T) {
		b, f, _ := newTestBackend(t)
		own := session.New("own", "/work/own", 0, 0)
		b.ownID = own.ID
		f.addTab(own, true)
		if err := b.l.store.Save(own); err != nil {
			t.Fatal(err)
		}
		f.calls = nil
		if err := b.PinWidth(36); err != nil || len(f.calls) != 0 {
			t.Errorf("err %v, calls %v; want nothing", err, f.calls)
		}
	})

	t.Run("home tab", func(t *testing.T) {
		home, fh, _ := newTestBackend(t)
		if err := home.PinWidth(80); err != nil || len(fh.calls) != 0 {
			t.Errorf("err %v, calls %v; want nothing", err, fh.calls)
		}
	})
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

	if err := b.New("", "/work/Fresh Dir", ""); err != nil {
		t.Fatalf("New: %v", err)
	}
	if !b.l.store.Exists("fresh-dir") {
		t.Error("New without a name did not derive one from the directory")
	}
	if err := b.New("fresh-dir", "/work/x", ""); !errors.Is(err, ErrExists) {
		t.Errorf("New with a taken name: %v, want ErrExists", err)
	}
	if err := b.New("", "", ""); err == nil {
		t.Error("New with nothing to name it from returned nil")
	}
	if err := b.New("pi-one", "/work/p", session.AgentPi); err != nil {
		t.Fatalf("New pi: %v", err)
	}
	if got, err := b.l.store.Load("pi-one"); err != nil || got.Agent != session.AgentPi {
		t.Errorf("New with a kind stored Agent %q (%v), want pi", got.Agent, err)
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

// TestHooksStatus checks the menu's hook line against the fake HOME, which
// has no Claude Code settings file: the hooks are injected, nothing is left
// over.
func TestHooksStatus(t *testing.T) {
	b, _, _ := newTestBackend(t)
	got, err := b.HooksStatus()
	if err != nil {
		t.Fatalf("HooksStatus: %v", err)
	}
	if want := "hooks injected at launch"; got != want {
		t.Errorf("HooksStatus = %q, want %q", got, want)
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

func TestFocused(t *testing.T) {
	b, f, _ := newTestBackend(t)
	sess := &session.Session{ID: "abc", Name: "demo", Dir: "/tmp/demo"}
	f.addTab(sess, true)
	f.find(sess.KittyWindowID).Focused = true // claude holds the keys

	tests := []struct {
		name   string
		env    string
		want   bool
		asksLs bool
	}{
		{"no window id", "", false, false},
		{"window id not a number", "two", false, false},
		{"window gone from the instance", "99", false, true},
		{"own window without focus", itoa(sess.KittySidebarWindowID), false, true},
		{"own window with focus", itoa(sess.KittyWindowID), true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("KITTY_WINDOW_ID", tt.env)
			before := countCalls(f, "Windows")
			got, err := b.Focused()
			if err != nil {
				t.Fatalf("Focused: %v", err)
			}
			if got != tt.want {
				t.Errorf("Focused() = %v, want %v", got, tt.want)
			}
			if asked := countCalls(f, "Windows") > before; asked != tt.asksLs {
				t.Errorf("asked kitty for a snapshot = %v, want %v", asked, tt.asksLs)
			}
		})
	}

	t.Setenv("KITTY_WINDOW_ID", itoa(sess.KittyWindowID))
	errSocket := errors.New("socket gone")
	f.errs["Windows"] = errSocket
	if _, err := b.Focused(); !errors.Is(err, errSocket) {
		t.Fatalf("Focused with a dead instance: err = %v, want %v wrapped", err, errSocket)
	}
}

func TestListReportsHowLongAnInputRowHasWaited(t *testing.T) {
	b, f, clock := newTestBackend(t)
	states := fakeStates{
		"asked": {"input", clock.Add(-30 * time.Minute)},
		"busy":  {"working", clock.Add(-30 * time.Minute)},
	}
	b.readState = states.read
	for _, name := range []string{"asked", "busy"} {
		s := session.New(name, "/work/"+name, 0, 0)
		f.addTab(s, true)
		f.find(s.KittyWindowID).Title = "✳ Claude Code"
		if err := b.l.store.Save(s); err != nil {
			t.Fatal(err)
		}
	}
	agents, err := b.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("got %d agents, want 2: %+v", len(agents), agents)
	}
	for _, a := range agents {
		switch a.Name {
		case "asked":
			if a.State != sidebar.StateInput || a.Waiting != 30*time.Minute {
				t.Errorf("asked = %s after %v, want input after 30m", a.State, a.Waiting)
			}
		case "busy":
			if a.Waiting != 0 {
				t.Errorf("busy waited %v, want 0", a.Waiting)
			}
		}
	}
}

// Screens in the shapes claude.ParseScreen reads, enough of each for the
// backstop to tell them apart.
const (
	promptBoxScreen = "─────────\n❯ \n─────────\n  ? for shortcuts\n"
	workingScreen   = "✻ Crunching… (3s · esc to interrupt)\n"
	trustScreen     = " Do you trust the files in this folder?\n" +
		" ❯ 1. Yes, proceed\n   2. No, exit\n Enter to confirm · Esc to exit\n"
	welcomeScreen = " ✻ Welcome to Claude Code!\n /help for help\n"
	// leftoverScreen is promptBoxScreen (four rows) under a spinner row left
	// in the scrollback.
	leftoverScreen = workingScreen + "\n\n" + promptBoxScreen
)

// backstopFixture is the own claude session of a backstop test: its state
// file and its screen, with the state and the writes expected.
type backstopFixture struct {
	kind    string
	status  kitty.ProgramState
	file    string        // the state file's state, "" for none
	fileAge time.Duration // the file's age against the backend's clock
	source  string        // the file's writer: "" for a hook, state.SourceScreen for backstop
	foreign bool          // the row belongs to another session
	lines   int           // the window's rows, zero for uncut text
	text    string
	readErr error
	// hookWrites restamps the state file on the second read of a poll, as a
	// hook writing between the poll's read and the correction would.
	hookWrites  bool
	want        sidebar.State
	wantWaiting time.Duration
	wantReads   int
	wantWrite   string // the state written, "" for none
}

// TestListScreenBackstop checks the own sidebar's screen check on its
// claude row: a stale input is cleared by the prompt box or a running turn
// and the correction written to the state file, a dialog on screen keeps
// it, an input is raised from a dialog when no hook has spoken yet or the
// file is backstop's own, a hook write in the meantime wins, and nothing is
// read for a fresh file, a state file the hooks own, a Program Status
// record, another session's row or another kind.
func TestListScreenBackstop(t *testing.T) {
	tests := []struct {
		name string
		backstopFixture
	}{
		{"stale input, prompt box clears it", backstopFixture{
			file: "input", fileAge: time.Hour, text: promptBoxScreen,
			want: sidebar.StateIdle, wantReads: 1, wantWrite: "idle",
		}},
		{"stale input, running turn clears it", backstopFixture{
			file: "input", fileAge: time.Hour, text: workingScreen,
			want: sidebar.StateWorking, wantReads: 1, wantWrite: "working",
		}},
		{"stale input, running turn with the prompt box drawn", backstopFixture{
			file: "input", fileAge: time.Hour, text: workingScreen + promptBoxScreen,
			want: sidebar.StateWorking, wantReads: 1, wantWrite: "working",
		}},
		{"stale input, dialog under a running turn keeps it", backstopFixture{
			file: "input", fileAge: time.Hour, text: workingScreen + trustScreen,
			want: sidebar.StateInput, wantWaiting: time.Hour, wantReads: 1,
		}},
		{"stale input, dialog on screen keeps it", backstopFixture{
			file: "input", fileAge: time.Hour, text: trustScreen,
			want: sidebar.StateInput, wantWaiting: time.Hour, wantReads: 1,
		}},
		{"stale input, read error keeps it", backstopFixture{
			file: "input", fileAge: time.Hour, readErr: errors.New("get-text: no such window"),
			want: sidebar.StateInput, wantWaiting: time.Hour, wantReads: 1,
		}},
		{"stale input, a hook wrote during the read", backstopFixture{
			file: "input", fileAge: time.Hour, text: promptBoxScreen, hookWrites: true,
			want: sidebar.StateInput, wantWaiting: time.Hour, wantReads: 1,
		}},
		{"stale input, spinner above a short screen is scrollback", backstopFixture{
			file: "input", fileAge: time.Hour, text: leftoverScreen, lines: 4,
			want: sidebar.StateIdle, wantReads: 1, wantWrite: "idle",
		}},
		{"fresh input is not read", backstopFixture{
			file: "input", fileAge: time.Second, text: promptBoxScreen,
			want: sidebar.StateInput, wantWaiting: time.Second,
		}},
		{"input from a record is not read", backstopFixture{
			status: kitty.ProgramBlocked, file: "input", fileAge: time.Hour,
			text: promptBoxScreen, want: sidebar.StateInput, wantWaiting: time.Hour,
		}},
		{"another session's row is not read", backstopFixture{
			foreign: true, file: "input", fileAge: time.Hour, text: promptBoxScreen,
			want: sidebar.StateInput, wantWaiting: time.Hour,
		}},
		{"no state file, trust dialog raises input", backstopFixture{
			text: trustScreen, want: sidebar.StateInput, wantReads: 1, wantWrite: "input",
		}},
		{"no state file, a hook wrote during the read", backstopFixture{
			text: trustScreen, hookWrites: true, want: sidebar.StateIdle, wantReads: 1,
		}},
		{"stale waiting, welcome screen writes nothing", backstopFixture{
			file: "waiting", fileAge: time.Hour, text: welcomeScreen,
			want: sidebar.StateIdle, wantReads: 1,
		}},
		{"fresh waiting is not read", backstopFixture{
			file: "waiting", fileAge: time.Second, text: trustScreen, want: sidebar.StateIdle,
		}},
		{"hook-written stale idle is not read", backstopFixture{
			file: "idle", fileAge: time.Hour, text: trustScreen, want: sidebar.StateIdle,
		}},
		{"hook-written stale working is not read", backstopFixture{
			file: "working", fileAge: time.Hour, text: trustScreen, want: sidebar.StateWorking,
		}},
		{"screen-written idle, dialog raises input", backstopFixture{
			file: "idle", fileAge: time.Hour, source: state.SourceScreen, text: trustScreen,
			want: sidebar.StateInput, wantReads: 1, wantWrite: "input",
		}},
		{"screen-written fresh idle, dialog raises input", backstopFixture{
			file: "idle", fileAge: time.Second, source: state.SourceScreen, text: trustScreen,
			want: sidebar.StateInput, wantReads: 1, wantWrite: "input",
		}},
		{"screen-written working, running turn writes nothing", backstopFixture{
			file: "working", fileAge: time.Hour, source: state.SourceScreen, text: workingScreen,
			want: sidebar.StateWorking, wantReads: 1,
		}},
		{"a shell row is not read", backstopFixture{
			kind: session.AgentShell, text: trustScreen, want: sidebar.StateIdle,
		}},
		{"a pi row is not read", backstopFixture{
			kind: session.AgentPi, text: trustScreen, want: sidebar.StateIdle,
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runBackstop(t, tt.backstopFixture)
		})
	}
}

// runBackstop lists one own session built from fx and checks the row, the
// text reads and the state writes against it.
func runBackstop(t *testing.T, fx backstopFixture) {
	t.Helper()
	type write struct{ name, state, source string }
	b, f, clock := newTestBackend(t)
	*clock = time.Now() // freshness is wall-clock; waiting is the backend's
	own := session.New("own", "/work/own", 0, 0)
	own.Agent = fx.kind
	f.addTab(own, true)
	w := f.find(own.KittyWindowID)
	w.Title, w.Status, w.Lines = "Claude Code", fx.status, fx.lines
	if err := b.l.store.Save(own); err != nil {
		t.Fatal(err)
	}
	if !fx.foreign {
		b.ownID = own.ID
	}
	stateReads := 0
	b.readState = func(name string) (state.Entry, error) {
		stateReads++
		if fx.hookWrites && stateReads > 1 {
			return state.Entry{State: "working", UpdatedAt: clock.Add(-time.Millisecond)}, nil
		}
		if name != "own" || fx.file == "" {
			return state.Entry{}, errors.New("no state file")
		}
		return state.Entry{
			State: fx.file, UpdatedAt: clock.Add(-fx.fileAge), Source: fx.source,
		}, nil
	}
	reads := 0
	b.readText = func(id int) (string, error) {
		reads++
		if id != own.KittyWindowID {
			t.Errorf("read window %d, want %d", id, own.KittyWindowID)
		}
		return fx.text, fx.readErr
	}
	var writes []write
	b.writeState = func(name string, e state.Entry) error {
		writes = append(writes, write{name, e.State, e.Source})
		return nil
	}

	agents, err := b.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1: %+v", len(agents), agents)
	}
	if a := agents[0]; a.State != fx.want || a.Waiting != fx.wantWaiting {
		t.Errorf("row = %s waiting %s, want %s waiting %s",
			a.State, a.Waiting, fx.want, fx.wantWaiting)
	}
	if reads != fx.wantReads {
		t.Errorf("read the text %d times, want %d", reads, fx.wantReads)
	}
	var wantWrites []write
	if fx.wantWrite != "" {
		// Every correction is marked as the screen check's own.
		wantWrites = []write{{"own", fx.wantWrite, state.SourceScreen}}
	}
	if !slices.Equal(writes, wantWrites) {
		t.Errorf("writes = %v, want %v", writes, wantWrites)
	}
}

// TestListScreenBackstopDebouncesRaiseReads checks that while no hook has
// written a state file the own row's text is read for a dialog at most once
// per raiseDebounce.
func TestListScreenBackstopDebouncesRaiseReads(t *testing.T) {
	b, f, clock := newTestBackend(t)
	own := session.New("own", "/work/own", 0, 0)
	b.ownID = own.ID
	f.addTab(own, true)
	f.find(own.KittyWindowID).Title = "Claude Code"
	if err := b.l.store.Save(own); err != nil {
		t.Fatal(err)
	}
	reads := 0
	b.readText = func(int) (string, error) { reads++; return welcomeScreen, nil }
	b.writeState = func(name string, e state.Entry) error {
		t.Errorf("wrote %s for %s on a welcome screen", e.State, name)
		return nil
	}
	list := func() {
		t.Helper()
		if _, err := b.List(); err != nil {
			t.Fatalf("List: %v", err)
		}
	}
	steps := []struct {
		advance   time.Duration
		wantReads int
	}{
		{0, 1},
		{0, 1},
		{raiseDebounce - time.Second, 1},
		{time.Second, 2},
		{raiseDebounce, 3},
	}
	for i, st := range steps {
		*clock = clock.Add(st.advance)
		list()
		if reads != st.wantReads {
			t.Errorf("step %d: %d reads, want %d", i, reads, st.wantReads)
		}
	}
}

// TestListScreenBackstopRaiseGateFollowsTheWriter checks the raise gate
// against the state file's last writer: open with no file, open while the
// file is backstop's own (so a claude whose hooks never fire keeps the
// screen check), shut by a hook's write, and open again for a hook's stale
// waiting but not a fresh one. The debounce is waited out between steps.
func TestListScreenBackstopRaiseGateFollowsTheWriter(t *testing.T) {
	b, f, clock := newTestBackend(t)
	*clock = time.Now() // freshness is wall-clock
	own := session.New("own", "/work/own", 0, 0)
	b.ownID = own.ID
	f.addTab(own, true)
	f.find(own.KittyWindowID).Title = "Claude Code"
	if err := b.l.store.Save(own); err != nil {
		t.Fatal(err)
	}
	var (
		file    *state.Entry // nil for no file
		reads   int
		noReads = 0
	)
	b.readState = func(string) (state.Entry, error) {
		if file == nil {
			return state.Entry{}, errors.New("no state file")
		}
		return *file, nil
	}
	b.readText = func(int) (string, error) { reads++; return welcomeScreen, nil }
	b.writeState = func(name string, e state.Entry) error {
		t.Errorf("wrote %s for %s on a welcome screen", e.State, name)
		return nil
	}
	steps := []struct {
		name  string
		file  *state.Entry
		reads int // reads this step adds
	}{
		{"no file", nil, 1},
		{"backstop's own idle", &state.Entry{
			State: "idle", UpdatedAt: clock.Add(-time.Hour), Source: state.SourceScreen,
		}, 1},
		{"backstop's own fresh working", &state.Entry{
			State: "working", UpdatedAt: clock.Add(-time.Second), Source: state.SourceScreen,
		}, 1},
		{"a hook's idle", &state.Entry{State: "idle", UpdatedAt: clock.Add(-time.Hour)}, noReads},
		{"a hook's fresh working", &state.Entry{
			State: "working", UpdatedAt: clock.Add(-time.Second),
		}, noReads},
		{"a hook's stale waiting", &state.Entry{
			State: "waiting", UpdatedAt: clock.Add(-time.Hour),
		}, 1},
		{"a hook's fresh waiting", &state.Entry{
			State: "waiting", UpdatedAt: clock.Add(-time.Second),
		}, noReads},
	}
	for _, st := range steps {
		file = st.file
		*clock = clock.Add(raiseDebounce)
		before := reads
		if _, err := b.List(); err != nil {
			t.Fatalf("%s: List: %v", st.name, err)
		}
		if got := reads - before; got != st.reads {
			t.Errorf("%s: %d reads, want %d", st.name, got, st.reads)
		}
	}
}

// TestTabPositionsCountPerOSWindow checks a tab's number is its position in
// its own OS window, every tab counted, the way goto_tab N counts: a tab
// detached into a second OS window starts at 1 again there.
func TestTabPositionsCountPerOSWindow(t *testing.T) {
	all := []kitty.Window{
		{ID: 1, TabID: 10, OSWindowID: 1},
		{ID: 2, TabID: 10, OSWindowID: 1},
		{ID: 3, TabID: 11, OSWindowID: 1}, // the user's own tab counts
		{ID: 4, TabID: 12, OSWindowID: 1},
		{ID: 5, TabID: 20, OSWindowID: 2},
		{ID: 6, TabID: 21, OSWindowID: 2},
	}
	want := map[int]int{10: 1, 11: 2, 12: 3, 20: 1, 21: 2}
	got := tabPositions(all)
	for tab, n := range want {
		if got[tab] != n {
			t.Errorf("tab %d numbered %d, want %d", tab, got[tab], n)
		}
	}
	if len(got) != len(want) {
		t.Errorf("numbered %d tabs, want %d", len(got), len(want))
	}
}
