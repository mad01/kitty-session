package launcher

import (
	"slices"
	"sync"
	"time"

	"github.com/mad01/kitty-session/internal/kitty"
)

// reapGrace is how long a session's sidebar waits for its claude window
// before it treats a tab that never got one as dead. Open launches the
// sidebar first and claude after it, and writes the window ids to the
// record last, so the sidebar's first ticks can find no claude window to
// match; a claude that exits right after launch (bad flags, a missing
// binary) never leaves one to find. Generous, because reaping a tab whose
// claude is still starting would cut the session short.
const reapGrace = 30 * time.Second

// reaper is a session sidebar's memory of its own claude window, for
// reapIfGone: whether a snapshot has shown the window, when the watch
// began, and whether the tab close has been issued. List runs off the UI
// loop and calls can overlap, so the lock keeps the decision to one tick.
type reaper struct {
	mu      sync.Mutex
	started time.Time // the first tick, zero until then
	seen    bool      // claude has been in a snapshot
	done    bool      // the tab close was issued
}

// due records one snapshot of the own session, tagged id, and reports
// whether its tab is to be closed on this tick: claude was seen and is
// gone, or was never seen and reapGrace has passed since the first tick.
// Another window of the session, a shell split or a claude relaunched
// beside the sidebar whose id the record does not carry yet, holds the tab
// open; a snapshot with no tab of the session at all leaves nothing to
// close. It answers true once: a tab that will not close is not retried.
func (r *reaper) due(now time.Time, all []kitty.Window, lv live, id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started.IsZero() {
		r.started = now
	}
	if r.done {
		return false
	}
	if lv.claude != nil {
		r.seen = true
		return false
	}
	if len(lv.tabs) == 0 || othersLive(all, lv, id) {
		return false
	}
	if !r.seen && now.Sub(r.started) < reapGrace {
		return false
	}
	r.done = true
	return true
}

// othersLive reports whether the snapshot holds a window tagged with the
// session id other than the sidebar and claude windows the record names
// and the window this process runs in, which KITTY_WINDOW_ID identifies
// before the record carries the sidebar's id.
func othersLive(all []kitty.Window, lv live, id string) bool {
	self, _ := ownWindowID()
	return slices.ContainsFunc(all, func(w kitty.Window) bool {
		return w.SessionID == id && w.ID != self &&
			!sameWindow(lv.sidebar, w.ID) && !sameWindow(lv.claude, w.ID)
	})
}

// sameWindow reports whether w is the window with id; a nil w is no window.
func sameWindow(w *kitty.Window, id int) bool { return w != nil && w.ID == id }

// reapIfGone closes this sidebar's own tab once the claude window beside it
// is gone, so a session whose claude ended, by /exit, a crash or a kill,
// does not leave a tab with the sidebar alone in it. The record and the
// state file stay as they are: the SessionEnd hook already marked an
// explicit exit stopped, and a crash leaves the record active so the next
// ks brings the session back. The home sidebar has no own tab and never
// reaps. Warnings are dropped as Close drops them: the UI has one status
// line, and this process dies with its tab.
func (b *SidebarBackend) reapIfGone(all []kitty.Window, lv live) {
	if b.ownID == "" || !b.reap.due(b.l.now(), all, lv, b.ownID) {
		return
	}
	b.l.reapTab(all, lv)
}

// reapTab closes every tab of a session whose claude window is gone, from
// the snapshot the caller holds, recreating the home tab first when they
// are the last session tabs, as Close does. Unlike Close it leaves the
// record and the state file alone: only the tab is dead.
func (l *Launcher) reapTab(all []kitty.Window, lv live) []error {
	if len(lv.tabs) == 0 {
		return nil
	}
	return l.closeTabs(all, lv)
}
