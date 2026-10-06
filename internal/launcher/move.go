package launcher

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/mad01/kitty-session/internal/kitty"
	"github.com/mad01/kitty-session/internal/session"
)

// MoveKind says how Move reads a MoveTarget.
type MoveKind int

const (
	// MoveTo puts the tab at MoveTarget.Position.
	MoveTo MoveKind = iota
	// MoveTop puts the tab first.
	MoveTop
	// MoveBottom puts the tab last.
	MoveBottom
	// MoveUp swaps the tab with the one before it.
	MoveUp
	// MoveDown swaps the tab with the one after it.
	MoveDown
)

// MoveTarget is where Move puts a session's tab. Position is read for MoveTo
// only. A target past either end of the tab list means that end.
type MoveTarget struct {
	Kind     MoveKind
	Position int // 1-based
}

// destination resolves the target for a tab at from among n tabs, clamped
// to 1..n.
func (t MoveTarget) destination(from, n int) int {
	to := from
	switch t.Kind {
	case MoveTo:
		to = t.Position
	case MoveTop:
		to = 1
	case MoveBottom:
		to = n
	case MoveUp:
		to = from - 1
	case MoveDown:
		to = from + 1
	}
	return min(max(to, 1), n)
}

// MoveResult reports what Move did.
type MoveResult struct {
	// From and To are the tab's 1-based positions before and after the
	// move, equal when the tab already was where it was asked to go.
	From, To int
	// Warnings are non-fatal problems met after the tab moved: the keyboard
	// not put back, or the new order not recorded on every session.
	Warnings []error
}

// Move puts the session's tab at the position where names, counted over
// every tab of the instance the way the sidebar counts them, and records the
// resulting order on each open session (Session.Position) so attach rebuilds
// it after ks quit. Only a session with a tab in the instance can move; a
// tab already in place is left alone and nothing is asked of kitty.
//
// kitty's move_tab_forward and move_tab_backward act on the active tab of
// the visible OS window whatever --match names (probed on kitty 0.48.2), so
// the move is: focus the session's tab unless it is the visible one, step it
// the needed number of times, then give the keyboard back to the window that
// had it. A tab moved from behind another is on screen for a moment.
func (l *Launcher) Move(name string, where MoveTarget) (*MoveResult, error) {
	sess, err := l.store.Load(name)
	if err != nil {
		return nil, err
	}
	if !sess.IsActive() {
		return nil, noOpenTab(name)
	}
	all, lv, err := l.snapshot(sess)
	if err != nil {
		return nil, fmt.Errorf("cannot list kitty windows: %w", err)
	}
	if lv.tabID() == 0 {
		return nil, noOpenTab(name)
	}
	pos := tabPositions(all)
	from := pos[lv.tabID()]
	res := &MoveResult{From: from, To: where.destination(from, len(pos))}
	if res.To == res.From {
		return res, nil
	}
	if err := l.showTab(all, lv); err != nil {
		return nil, err
	}
	if err := l.kitty.MoveActiveTab(res.To - res.From); err != nil {
		return nil, fmt.Errorf("cannot move the tab: %w", err)
	}
	res.Warnings = append(l.returnKeyboard(all, lv), l.recordOrder()...)
	return res, nil
}

func noOpenTab(name string) error {
	return fmt.Errorf("session %q has no open tab", name)
}

// showTab makes the session's tab the active one, which the tab-move actions
// act on; a tab that is already showing needs nothing.
func (l *Launcher) showTab(all []kitty.Window, lv live) error {
	if lv.tabActive() {
		return nil
	}
	if err := l.kitty.FocusWindow(moveAnchor(all, lv)); err != nil {
		return fmt.Errorf("cannot focus the tab: %w", err)
	}
	return nil
}

// moveAnchor is the window focused to bring the session's tab up: claude,
// so a later switch to the tab lands where the sidebar's enter does, else
// the sidebar, else whichever tagged window the tab still holds.
func moveAnchor(all []kitty.Window, lv live) int {
	if lv.claude != nil {
		return lv.claude.ID
	}
	if lv.sidebar != nil {
		return lv.sidebar.ID
	}
	i := slices.IndexFunc(all, func(w kitty.Window) bool { return w.TabID == lv.tabID() })
	return all[i].ID
}

// returnKeyboard focuses the window that had the keyboard before showTab,
// from the snapshot taken before the move: the focused window, else the
// first window of the tab that was showing (the OS window had no keyboard
// focus). Nothing to do when the session's own tab was the one showing.
func (l *Launcher) returnKeyboard(all []kitty.Window, lv live) []error {
	if lv.tabActive() {
		return nil
	}
	back := slices.IndexFunc(all, func(w kitty.Window) bool { return w.Focused })
	if back < 0 {
		back = slices.IndexFunc(all, func(w kitty.Window) bool { return w.TabActive })
	}
	if back < 0 {
		return nil
	}
	if err := l.kitty.FocusWindow(all[back].ID); err != nil {
		return []error{fmt.Errorf("could not give the keyboard back: %w", err)}
	}
	return nil
}

// recordOrder ranks every active session that has a tab by the instance's
// tab order, from a fresh snapshot, and saves the records whose rank changed.
// Move runs it after every move and Open after every launch once a rank
// exists (rerank), so the stored ranks never disagree with kitty's order: a
// session whose tab is gone keeps its rank only until the next launch or
// move.
func (l *Launcher) recordOrder() []error {
	sessions, err := l.store.List()
	if err != nil {
		return []error{fmt.Errorf("order not recorded: %w", err)}
	}
	return l.rankSessions(sessions)
}

// rerank is recordOrder for a launch: it runs only once some active session
// carries a rank, so a store that never saw a ks move stays unranked and a
// user who never moved a tab sees no record churn. With a rank in place a
// reopened session takes the bottom rank and a new one gets the next one.
func (l *Launcher) rerank() []error {
	sessions, err := l.store.List()
	if err != nil {
		return []error{fmt.Errorf("order not recorded: %w", err)}
	}
	if !slices.ContainsFunc(sessions, ranked) {
		return nil
	}
	return l.rankSessions(sessions)
}

// ranked reports whether an active session carries a rank from a ks move.
func ranked(sess *session.Session) bool {
	return sess.IsActive() && sess.Position > 0
}

// rankSessions takes a snapshot and saves every open session whose rank in
// the tab order differs from the stored one.
func (l *Launcher) rankSessions(sessions []*session.Session) []error {
	all, err := l.kitty.Windows()
	if err != nil {
		return []error{fmt.Errorf("order not recorded: %w", err)}
	}
	var warnings []error
	for rank, sess := range openByTab(all, sessions) {
		if sess.Position == rank+1 {
			continue
		}
		sess.Position = rank + 1
		if err := l.store.Save(sess); err != nil {
			warnings = append(warnings, fmt.Errorf("order not recorded for %s: %w", sess.Name, err))
		}
	}
	return warnings
}

// openByTab returns the active sessions with a tab in the snapshot, in the
// order of their tabs.
func openByTab(all []kitty.Window, sessions []*session.Session) []*session.Session {
	pos := tabPositions(all)
	tab := map[*session.Session]int{}
	var open []*session.Session
	for _, sess := range sessions {
		if t := findLive(all, sess).tabID(); sess.IsActive() && t != 0 {
			tab[sess] = pos[t]
			open = append(open, sess)
		}
	}
	slices.SortFunc(open, func(a, b *session.Session) int { return cmp.Compare(tab[a], tab[b]) })
	return open
}
