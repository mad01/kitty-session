package launcher

import (
	"fmt"
	"os"
	"slices"

	"github.com/mad01/kitty-session/internal/kitty"
	"github.com/mad01/kitty-session/internal/session"
)

// backend is the kitty surface the launcher drives. *kitty.Client is the
// production backend; tests substitute a fake so nothing shells out.
type backend interface {
	Windows() ([]kitty.Window, error)
	LaunchTab(kitty.Launch) (int, error)
	LaunchVSplit(kitty.Launch) (int, error)
	LaunchHSplit(kitty.Launch) (int, error)
	GotoLayout(windowID int, layout string) error
	ResizeWindow(windowID int, axis string, increment int) error
	SetTabTitleForWindow(title string, windowID int) error
	FocusWindow(windowID int) error
	CloseTab(tabID int) error
}

// Geometry constants of a session tab.
const (
	// defaultClaudeBias is claude's share of the tab when the sidebar's
	// width cannot be measured before the split.
	defaultClaudeBias = 75
	// minBias and maxBias bound the split share kitty accepts.
	minBias = 10
	maxBias = 90
	// pinPasses is how many measure-and-resize rounds settle the sidebar
	// width. Splits are fractions and kitty rounds a resize, so the first
	// pass can leave a cell; the second corrects it.
	pinPasses = 2
	// homeTitle is the tab title of the home tab: kitty's first tab, and the
	// one closeTabs recreates before the last session tab goes.
	homeTitle = "ks"
)

// plan is everything launchTopology needs to lay out one session.
type plan struct {
	name       string
	dir        string
	env        []string // KEY=VALUE for both windows
	vars       []string // kitty user variables tagging both windows
	sidebarCmd []string
	claudeCmd  []string
}

// windows holds the kitty IDs a topology produced plus any non-fatal problems
// met on the way.
type windows struct {
	tabID     int
	sidebarID int
	claudeID  int
	warnings  []error
}

// live is the set of a session's windows found in one instance snapshot.
type live struct {
	claude  *kitty.Window // nil when the claude window is gone
	sidebar *kitty.Window // nil when the sidebar window is gone
	tabs    []int         // every tab holding a window the session owns
}

// tabID is the tab holding the session's windows, zero when it has none.
func (lv live) tabID() int {
	if len(lv.tabs) == 0 {
		return 0
	}
	return lv.tabs[0]
}

// tabActive reports whether the session's tab is the one its OS window shows.
func (lv live) tabActive() bool {
	for _, w := range []*kitty.Window{lv.sidebar, lv.claude} {
		if w != nil && w.TabActive {
			return true
		}
	}
	return false
}

// ClaudeWindow returns sess's claude window from an already-taken snapshot,
// matched by the session tag (never a stored id alone), and whether it is
// present. A command that already holds a Windows() snapshot resolves state
// through this instead of taking one snapshot per session.
func ClaudeWindow(all []kitty.Window, sess *session.Session) (kitty.Window, bool) {
	lv := findLive(all, sess)
	if lv.claude == nil {
		return kitty.Window{}, false
	}
	return *lv.claude, true
}

// liveWindows takes a snapshot and picks out the session's windows.
func (l *Launcher) liveWindows(sess *session.Session) (live, error) {
	_, lv, err := l.snapshot(sess)
	return lv, err
}

// snapshot takes one Windows() snapshot and returns it along with the
// session's windows picked out of it, for callers that also need to look at
// the rest of the instance.
func (l *Launcher) snapshot(sess *session.Session) ([]kitty.Window, live, error) {
	all, err := l.kitty.Windows()
	if err != nil {
		return nil, live{}, err
	}
	return all, findLive(all, sess), nil
}

// findLive matches windows to sess by the SessionVar tag, never by id alone:
// kitty numbers windows from 1 in every instance, so a stored id can point at
// another session's window after a restart. A record without an ID owns
// nothing, since every ks launch tags its windows.
func findLive(all []kitty.Window, sess *session.Session) live {
	var lv live
	if sess.ID == "" {
		return lv
	}
	for i := range all {
		w := &all[i]
		if w.SessionID != sess.ID {
			continue
		}
		if w.ID == sess.KittyWindowID {
			lv.claude = w
		}
		if w.ID == sess.KittySidebarWindowID {
			lv.sidebar = w
		}
		if !slices.Contains(lv.tabs, w.TabID) {
			lv.tabs = append(lv.tabs, w.TabID)
		}
	}
	return lv
}

// window returns the window with id from a fresh snapshot.
func (l *Launcher) window(id int) (kitty.Window, error) {
	all, err := l.kitty.Windows()
	if err != nil {
		return kitty.Window{}, err
	}
	i := slices.IndexFunc(all, func(w kitty.Window) bool { return w.ID == id })
	if i < 0 {
		return kitty.Window{}, fmt.Errorf("window %d: %w", id, kitty.ErrNotFound)
	}
	return all[i], nil
}

// anyWindow returns the id of the first window in the instance, the anchor
// for creating tabs in its OS window.
func (l *Launcher) anyWindow() (int, error) {
	all, err := l.kitty.Windows()
	if err != nil {
		return 0, err
	}
	if len(all) == 0 {
		return 0, fmt.Errorf("instance has no windows: %w", kitty.ErrNotFound)
	}
	return all[0].ID, nil
}

// launchTopology lays out one session as a tab in the instance: a sidebar
// window running `ks sidebar` on the left and claude on the right. The tab
// is built behind the current one (every launch keeps the keyboard where it
// is) so the user sees it only once it has settled; Open shows it. A new tab
// opens in the first enabled layout, so it is switched to splits before the
// vertical split, otherwise kitty ignores the location.
func (l *Launcher) launchTopology(p plan) (windows, error) {
	var w windows
	anchor, err := l.anyWindow()
	if err != nil {
		return w, fmt.Errorf("cannot find a window to anchor the tab on: %w", err)
	}
	w.sidebarID, err = l.kitty.LaunchTab(kitty.Launch{
		Match: anchor, Dir: p.dir, Env: p.env, Vars: p.vars, Command: p.sidebarCmd,
		KeepFocus: true,
	})
	if err != nil {
		return w, fmt.Errorf("cannot create tab: %w", err)
	}
	if err := l.kitty.GotoLayout(w.sidebarID, kitty.LayoutSplits); err != nil {
		return w, fmt.Errorf("cannot set tab layout: %w", err)
	}
	if err := l.kitty.SetTabTitleForWindow(p.name, w.sidebarID); err != nil {
		return w, fmt.Errorf("cannot set tab title: %w", err)
	}
	sidebar, err := l.window(w.sidebarID)
	if err != nil {
		return w, fmt.Errorf("cannot find the new tab: %w", err)
	}
	w.tabID = sidebar.TabID
	w.claudeID, w.warnings, err = l.splitClaude(p, sidebar)
	return w, err
}

// relaunchClaude puts claude back into a tab whose sidebar survived. The
// layout is set again in case the user changed it.
func (l *Launcher) relaunchClaude(p plan, sidebar kitty.Window) (windows, error) {
	w := windows{tabID: sidebar.TabID, sidebarID: sidebar.ID}
	if err := l.kitty.GotoLayout(sidebar.ID, kitty.LayoutSplits); err != nil {
		return w, fmt.Errorf("cannot set tab layout: %w", err)
	}
	var err error
	w.claudeID, w.warnings, err = l.splitClaude(p, sidebar)
	return w, err
}

// splitClaude launches claude beside the sidebar and pins the sidebar's
// width, both out of sight: the launch keeps the keyboard where it is, and
// kitty lays out and resizes hidden tabs all the same. From the sidebar,
// --location=vsplit puts claude on the right, so no layout action is needed;
// one aimed at a hidden tab would act on the visible tab instead. The
// sidebar spans the tab when this runs, so its width is the tab's width.
// Geometry failures are warnings; the session works without them.
func (l *Launcher) splitClaude(p plan, sidebar kitty.Window) (int, []error, error) {
	claudeID, err := l.kitty.LaunchVSplit(kitty.Launch{
		Match: sidebar.ID, Dir: p.dir, Bias: l.claudeBias(sidebar.Columns),
		Env: p.env, Vars: p.vars, Command: p.claudeCmd, KeepFocus: true,
	})
	if err != nil {
		return 0, nil, fmt.Errorf("cannot create claude window: %w", err)
	}
	return claudeID, l.pinSidebar(sidebar.ID), nil
}

// claudeBias returns claude's share of a tab totalColumns wide, in percent,
// so that the sidebar is left with its configured width.
func (l *Launcher) claudeBias(totalColumns int) int {
	if totalColumns <= 0 {
		return defaultClaudeBias
	}
	bias := (totalColumns - l.sidebarWidth) * 100 / totalColumns
	return min(max(bias, minBias), maxBias)
}

// pinSidebar resizes the sidebar to its configured width.
func (l *Launcher) pinSidebar(sidebarID int) []error {
	for range pinPasses {
		w, err := l.window(sidebarID)
		if err != nil {
			return []error{fmt.Errorf("could not measure sidebar: %w", err)}
		}
		delta := l.sidebarWidth - w.Columns
		if delta == 0 {
			return nil
		}
		if err := l.kitty.ResizeWindow(sidebarID, kitty.AxisHorizontal, delta); err != nil {
			return []error{fmt.Errorf("could not resize sidebar: %w", err)}
		}
	}
	return nil
}

// closeTabs closes every tab holding a window the session owns. When those
// are the last session tabs and there is no home tab, the home tab is
// created first: the instance quits with its last window. A tab that will
// not close is a warning, since the session is being torn down regardless.
func (l *Launcher) closeTabs(all []kitty.Window, lv live) []error {
	var warnings []error
	if len(lv.tabs) > 0 && !sessionTabsOutside(all, lv.tabs) && len(homeTabs(all)) == 0 {
		if err := l.openHome(all); err != nil {
			warnings = append(warnings, fmt.Errorf("could not recreate the home tab: %w", err))
		}
	}
	for _, tab := range lv.tabs {
		if err := l.kitty.CloseTab(tab); err != nil {
			warnings = append(warnings, fmt.Errorf("could not close tab %d: %w", tab, err))
		}
	}
	return warnings
}

// homeTabs returns the tabs holding no session-tagged window, in instance
// order: the home tab, whether kitty's first tab or one openHome made. ks
// tags every window it launches for a session, so an untagged tab is not a
// session's.
func homeTabs(all []kitty.Window) []int {
	var order []int
	tagged := map[int]bool{}
	for _, w := range all {
		if !slices.Contains(order, w.TabID) {
			order = append(order, w.TabID)
		}
		if w.SessionID != "" {
			tagged[w.TabID] = true
		}
	}
	var home []int
	for _, tab := range order {
		if !tagged[tab] {
			home = append(home, tab)
		}
	}
	return home
}

// agentHome reports whether a window of the tab carries the --agent tag.
func agentHome(all []kitty.Window, tab int) bool {
	return slices.ContainsFunc(all, func(w kitty.Window) bool {
		return w.TabID == tab && w.HomeAgent
	})
}

// sessionTabsOutside reports whether a session-tagged window lives in a tab
// other than those in closing. With closing nil it asks whether any session
// tab exists at all.
func sessionTabsOutside(all []kitty.Window, closing []int) bool {
	return slices.ContainsFunc(all, func(w kitty.Window) bool {
		return w.SessionID != "" && !slices.Contains(closing, w.TabID)
	})
}

// openHome creates the home tab: `ks sidebar` with no session, titled
// homeTitle, anchored on any window of the snapshot.
func (l *Launcher) openHome(all []kitty.Window) error {
	if len(all) == 0 {
		return fmt.Errorf("instance has no windows: %w", kitty.ErrNotFound)
	}
	id, err := l.kitty.LaunchTab(kitty.Launch{
		Match:   all[0].ID,
		Env:     []string{"PATH=" + os.Getenv("PATH")},
		Command: []string{l.exe, "sidebar"},
	})
	if err != nil {
		return fmt.Errorf("cannot create the home tab: %w", err)
	}
	if err := l.kitty.SetTabTitleForWindow(homeTitle, id); err != nil {
		return fmt.Errorf("cannot title the home tab: %w", err)
	}
	return nil
}

// retireHome takes a snapshot and closes the home tab if a session tab
// exists; see retireHomeIn.
func (l *Launcher) retireHome() []error {
	all, err := l.kitty.Windows()
	if err != nil {
		return []error{fmt.Errorf("home tab left as it is: %w", err)}
	}
	return l.retireHomeIn(all)
}

// retireHomeIn closes every home tab in the snapshot once a session tab
// exists, except one whose sidebar runs the --agent monitor. The home tab is
// only there so the instance has a window while no session does; with the
// tab bar hidden, a goto_tab key would otherwise land on its blank right half.
func (l *Launcher) retireHomeIn(all []kitty.Window) []error {
	if !sessionTabsOutside(all, nil) {
		return nil
	}
	var warnings []error
	for _, tab := range homeTabs(all) {
		if agentHome(all, tab) {
			continue
		}
		if err := l.kitty.CloseTab(tab); err != nil {
			warnings = append(warnings, fmt.Errorf("could not close the home tab: %w", err))
		}
	}
	return warnings
}
