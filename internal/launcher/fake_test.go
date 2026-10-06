package launcher

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mad01/kitty-session/internal/kitty"
	"github.com/mad01/kitty-session/internal/session"
)

// fakeTabColumns is the width of a fresh tab. 158 is a real laptop value and
// makes the 36-cell sidebar land one cell wide after the split, so the pin
// pass has work to do.
const fakeTabColumns = 158

// fakeKitty is an in-memory instance: it records the call sequence and keeps
// a window table realistic enough for the launcher's geometry logic.
type fakeKitty struct {
	windows     []kitty.Window
	nextWindow  int
	nextTab     int
	calls       []string
	launches    []kitty.Launch
	errs        map[string]error // method name or exact call → error to return
	onLaunch    func()           // runs inside LaunchTab, standing in for the SessionStart hook
	onWindows   func() error     // runs inside Windows, to fail one snapshot among several
	claudeExits bool             // a window launched by LaunchVSplit vanishes at once
	slept       time.Duration
}

// newFakeKitty returns an instance holding only the home tab, its sidebar
// tagged as the real one tags itself at startup.
func newFakeKitty() *fakeKitty {
	return &fakeKitty{
		windows: []kitty.Window{
			{ID: 1, TabID: 1, TabTitle: "ks", Title: "ks", Columns: fakeTabColumns, Home: true},
		},
		nextWindow: 2,
		nextTab:    2,
		errs:       map[string]error{},
	}
}

// addTab registers a session tab for sess: a sidebar window and, when
// withClaude, a claude window beside it, both tagged with the session id.
// The record's kitty IDs are updated to match.
func (f *fakeKitty) addTab(sess *session.Session, withClaude bool) {
	tab := f.nextTab
	f.nextTab++
	sidebar := kitty.Window{
		ID: f.nextWindow, TabID: tab, TabTitle: sess.Name, Title: "ks",
		Columns: fakeTabColumns, SessionID: sess.ID,
	}
	f.nextWindow++
	sess.KittyTabID, sess.KittySidebarWindowID = tab, sidebar.ID
	if withClaude {
		claude := kitty.Window{
			ID: f.nextWindow, TabID: tab, TabTitle: sess.Name, Title: "✳ claude",
			Columns: fakeTabColumns - 36, SessionID: sess.ID,
		}
		f.nextWindow++
		sidebar.Columns = 36
		sess.KittyWindowID = claude.ID
		f.windows = append(f.windows, sidebar, claude)
		return
	}
	f.windows = append(f.windows, sidebar)
}

// addUserTab registers a plain shell tab the user opened in the instance
// (cmd+t): one window with no ks tag. It returns the tab id.
func (f *fakeKitty) addUserTab() int {
	tab := f.nextTab
	f.nextTab++
	f.windows = append(f.windows, kitty.Window{
		ID: f.nextWindow, TabID: tab, TabTitle: "zsh", Title: "zsh", Columns: fakeTabColumns,
	})
	f.nextWindow++
	return tab
}

// hasTab reports whether any window of the tab is still in the instance.
func (f *fakeKitty) hasTab(tab int) bool {
	return slices.ContainsFunc(f.windows, func(w kitty.Window) bool { return w.TabID == tab })
}

func (f *fakeKitty) record(format string, args ...any) error {
	call := fmt.Sprintf(format, args...)
	f.calls = append(f.calls, call)
	if err, ok := f.errs[call]; ok {
		return err
	}
	name, _, _ := strings.Cut(call, "(")
	return f.errs[name]
}

func (f *fakeKitty) find(id int) *kitty.Window {
	i := slices.IndexFunc(f.windows, func(w kitty.Window) bool { return w.ID == id })
	if i < 0 {
		return nil
	}
	return &f.windows[i]
}

func (f *fakeKitty) Windows() ([]kitty.Window, error) {
	if err := f.record("Windows"); err != nil {
		return nil, err
	}
	if f.onWindows != nil {
		if err := f.onWindows(); err != nil {
			return nil, err
		}
	}
	return slices.Clone(f.windows), nil
}

func (f *fakeKitty) LaunchTab(l kitty.Launch) (int, error) {
	if err := f.record("LaunchTab"); err != nil {
		return 0, err
	}
	f.launches = append(f.launches, l)
	if f.onLaunch != nil {
		f.onLaunch()
	}
	id := f.nextWindow
	f.nextWindow++
	f.windows = append(f.windows, kitty.Window{
		ID: id, TabID: f.nextTab, Columns: fakeTabColumns,
		SessionID: varValue(l.Vars, kitty.SessionVar),
		Home:      varValue(l.Vars, kitty.HomeVar) != "",
	})
	f.nextTab++
	return id, nil
}

func (f *fakeKitty) LaunchVSplit(l kitty.Launch) (int, error) {
	if err := f.record("LaunchVSplit(%d,bias=%d)", l.Match, l.Bias); err != nil {
		return 0, err
	}
	f.launches = append(f.launches, l)
	target := f.find(l.Match)
	if target == nil {
		return 0, fmt.Errorf("fake: no window %d", l.Match)
	}
	id := f.nextWindow
	f.nextWindow++
	newCols := target.Columns * l.Bias / 100
	target.Columns -= newCols
	if f.claudeExits {
		target.Columns += newCols // the split collapses again
		return id, nil
	}
	f.windows = append(f.windows, kitty.Window{
		ID: id, TabID: target.TabID, TabTitle: target.TabTitle,
		Columns: newCols, SessionID: varValue(l.Vars, kitty.SessionVar),
	})
	return id, nil
}

func (f *fakeKitty) LaunchHSplit(l kitty.Launch) (int, error) {
	if err := f.record("LaunchHSplit(%d,bias=%d)", l.Match, l.Bias); err != nil {
		return 0, err
	}
	f.launches = append(f.launches, l)
	target := f.find(l.Match)
	if target == nil {
		return 0, fmt.Errorf("fake: no window %d", l.Match)
	}
	id := f.nextWindow
	f.nextWindow++
	f.windows = append(f.windows, kitty.Window{
		ID: id, TabID: target.TabID, TabTitle: target.TabTitle,
		Columns: target.Columns, SessionID: varValue(l.Vars, kitty.SessionVar),
	})
	return id, nil
}

func (f *fakeKitty) GotoLayout(id int, layout string) error {
	return f.record("GotoLayout(%d,%s)", id, layout)
}

func (f *fakeKitty) ResizeWindow(id int, axis string, increment int) error {
	if err := f.record("ResizeWindow(%d,%s,%d)", id, axis, increment); err != nil {
		return err
	}
	if w := f.find(id); w != nil {
		w.Columns += increment
	}
	return nil
}

func (f *fakeKitty) SetTabTitleForWindow(title string, id int) error {
	if err := f.record("SetTabTitle(%d,%s)", id, title); err != nil {
		return err
	}
	if w := f.find(id); w != nil {
		for i := range f.windows {
			if f.windows[i].TabID == w.TabID {
				f.windows[i].TabTitle = title
			}
		}
	}
	return nil
}

func (f *fakeKitty) FocusWindow(id int) error {
	if err := f.record("FocusWindow(%d)", id); err != nil {
		return err
	}
	f.focus(id)
	return nil
}

// focus gives window id the keyboard and shows its tab, as kitty does after
// focus-window; an unknown id changes nothing. Nothing is recorded.
func (f *fakeKitty) focus(id int) {
	w := f.find(id)
	if w == nil {
		return
	}
	tab := w.TabID
	for i := range f.windows {
		f.windows[i].Focused = f.windows[i].ID == id
		f.windows[i].TabActive = f.windows[i].TabID == tab
	}
}

// MoveActiveTab moves the showing tab by steps in the tab order, clamped to
// the ends, by regrouping the window table in the new order.
func (f *fakeKitty) MoveActiveTab(steps int) error {
	if err := f.record("MoveActiveTab(%d)", steps); err != nil {
		return err
	}
	var order []int
	active := -1
	for _, w := range f.windows {
		if slices.Contains(order, w.TabID) {
			continue
		}
		if w.TabActive {
			active = len(order)
		}
		order = append(order, w.TabID)
	}
	if active < 0 {
		return fmt.Errorf("fake: no tab is showing")
	}
	to := min(max(active+steps, 0), len(order)-1)
	tab := order[active]
	order = slices.Insert(slices.Delete(order, active, active+1), to, tab)
	var regrouped []kitty.Window
	for _, t := range order {
		for _, w := range f.windows {
			if w.TabID == t {
				regrouped = append(regrouped, w)
			}
		}
	}
	f.windows = regrouped
	return nil
}

func (f *fakeKitty) CloseTab(tab int) error {
	if err := f.record("CloseTab(%d)", tab); err != nil {
		return err
	}
	f.windows = slices.DeleteFunc(f.windows, func(w kitty.Window) bool { return w.TabID == tab })
	return nil
}

// varValue returns the value of the named user variable in a launch, empty
// when it is not set.
func varValue(vars []string, name string) string {
	for _, v := range vars {
		if value, ok := strings.CutPrefix(v, name+"="); ok {
			return value
		}
	}
	return ""
}

// newTestLauncher wires a store in a fresh HOME, the fake instance, a fixed
// clock that advances a minute per call, and a sleep counter.
func newTestLauncher(t *testing.T) (*Launcher, *fakeKitty, *session.Store) {
	t.Helper()
	store := newTestStore(t)
	f := newFakeKitty()
	l := newLauncher(store, f, 36, "/bin/ks")
	clock := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time {
		clock = clock.Add(time.Minute)
		return clock
	}
	l.sleep = func(d time.Duration) { f.slept += d }
	return l, f, store
}

func newTestStore(t *testing.T) *session.Store {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	store, err := session.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	return store
}

// newTabLaunch is the call sequence that lays out a fresh session tab in an
// instance whose tabs are fakeTabColumns wide: sidebar window 2, claude 3.
// Both launches keep focus; the tab is shown by the final FocusWindow.
// The split leaves the sidebar at 37 cells, so the pin shrinks it by one and
// the second pass finds nothing to do.
func newTabLaunch(sidebar, claude int, name string) []string {
	return []string{
		"Windows", // anchor
		"LaunchTab",
		fmt.Sprintf("GotoLayout(%d,splits)", sidebar),
		fmt.Sprintf("SetTabTitle(%d,%s)", sidebar, name),
		"Windows", // tab id and width
		fmt.Sprintf("LaunchVSplit(%d,bias=77)", sidebar),
		"Windows", // pin pass 1
		fmt.Sprintf("ResizeWindow(%d,horizontal,-1)", sidebar),
		"Windows", // pin pass 2
		fmt.Sprintf("FocusWindow(%d)", claude),
	}
}

func hasEnv(l kitty.Launch, want string) bool { return slices.Contains(l.Env, want) }

func checkIDs(t *testing.T, got *session.Session, tab, sidebar, claude int) {
	t.Helper()
	if got.KittyTabID != tab {
		t.Errorf("KittyTabID = %d, want %d", got.KittyTabID, tab)
	}
	if got.KittySidebarWindowID != sidebar {
		t.Errorf("KittySidebarWindowID = %d, want %d", got.KittySidebarWindowID, sidebar)
	}
	if got.KittyWindowID != claude {
		t.Errorf("KittyWindowID = %d, want %d", got.KittyWindowID, claude)
	}
	if got.KittyShellWindowID != 0 || got.KittySummaryWindowID != 0 {
		t.Errorf("legacy window ids kept: shell %d summary %d",
			got.KittyShellWindowID, got.KittySummaryWindowID)
	}
}
