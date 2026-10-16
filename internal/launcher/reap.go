package launcher

import (
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"sync"
	"time"

	"github.com/mad01/kitty-session/internal/kitty"
	"github.com/mad01/kitty-session/internal/session"
)

// reapGrace is how long a session's sidebar waits for its agent window
// before it treats a tab that never got one as dead. Open launches the
// sidebar first and the agent after it, and writes the window ids to the
// record last, so the sidebar's first ticks can find no agent window to
// match; a claude that exits right after launch (bad flags, a missing
// binary) never leaves one to find. Generous, because reaping a tab whose
// agent is still starting would cut the session short.
const reapGrace = 30 * time.Second

// reapRan is how long before the tick that finds the agent window gone a
// snapshot must first have shown it for its going to count as the agent
// having run: a session the user ended, whose record goes to the trash. A
// window gone sooner is a start that failed (an expired login, a purged
// transcript, a broken install), and its record stays for the next attach
// to try again, as a window never seen does. The hook's stopped status
// outranks the span: a claude the user told to exit is ended however short
// its run.
const reapRan = 10 * time.Second

// pokeLooks is how many times Poke looks for the own agent window, and
// pokePoll how long it waits between looks: twenty looks a quarter second
// apart cover the moments an agent takes to exit after its hook ran.
const (
	pokeLooks = 20
	pokePoll  = 250 * time.Millisecond
)

// reaper is a session sidebar's memory of its own agent window, for
// reapIfGone: which window the snapshots have shown and since when, when
// the watch began, and whether the tab close has been issued. List runs
// off the UI loop and calls can overlap, so the lock keeps the decision to
// one tick.
type reaper struct {
	mu        sync.Mutex
	started   time.Time // the first tick, zero until then
	window    int       // the agent window the snapshots showed, 0 before one did
	firstSeen time.Time // the tick that first showed it
	done      bool      // the tab close was issued
}

// verdict is what due reports for one tick.
type verdict struct {
	fire   bool // close the tab on this tick
	ran    bool // a snapshot first showed the agent window reapRan or more ago
	window int  // the agent window the snapshots showed, 0 when none did
}

// due records one snapshot of the own session, tagged id, and reports
// whether its tab is to be closed on this tick. It fires when the agent was
// seen and is gone, so it ended, or was never seen and reapGrace has passed
// since the first tick, so it did not come up. Another window of the
// session, a shell split or an agent relaunched beside the sidebar whose id
// the record does not carry yet, holds the tab open; a snapshot with no tab
// of the session at all leaves nothing to close. It fires once: a tab that
// will not close is not retried. A new agent window beside the sidebar, a
// relaunch, starts the span over.
func (r *reaper) due(now time.Time, all []kitty.Window, lv live, id string) verdict {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started.IsZero() {
		r.started = now
	}
	if r.done {
		return r.report(now, false)
	}
	if lv.claude != nil {
		if lv.claude.ID != r.window {
			r.window, r.firstSeen = lv.claude.ID, now
		}
		return r.report(now, false)
	}
	if len(lv.tabs) == 0 || othersLive(all, lv, id) {
		return r.report(now, false)
	}
	if r.window == 0 && now.Sub(r.started) < reapGrace {
		return r.report(now, false)
	}
	r.done = true
	return r.report(now, true)
}

// fired reports whether the tab close has been issued.
func (r *reaper) fired() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.done
}

// report gives the watch as of now; fire says whether the tab closes now.
func (r *reaper) report(now time.Time, fire bool) verdict {
	return verdict{
		fire:   fire,
		ran:    r.window != 0 && now.Sub(r.firstSeen) >= reapRan,
		window: r.window,
	}
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

// reapIfGone closes this sidebar's own tab once the agent window beside it
// is gone, so a session whose agent ended, by /exit, a crash or a kill,
// does not leave a tab with the sidebar alone in it, and settles the record
// first (reapRecord). sess is the own record the List tick holds. The home
// sidebar has no own tab and never reaps. The record step runs under the
// backend lock, so an overlapping tick's markViewed cannot write the record
// back after it is trashed. Its error is dropped as Close drops warnings:
// the UI has one status line, and this process dies with its tab. The
// order inside trash keeps that benign: the record is marked stopped before
// it is moved, so one the store cannot move stays as a stopped row the next
// attach leaves alone.
func (b *SidebarBackend) reapIfGone(all []kitty.Window, lv live, sess *session.Session) {
	if b.ownID == "" {
		return
	}
	v := b.reap.due(b.l.now(), all, lv, b.ownID)
	if !v.fire {
		return
	}
	// The sidebar window posted the session's notification and goes with
	// the tab, so the notification is taken down while it still can be.
	if b.withdraw != nil {
		b.withdraw()
	}
	b.mu.Lock()
	_ = b.l.reapRecord(sess, v)
	b.mu.Unlock()
	b.l.closeTabs(all, lv)
}

// reapRecord settles the record of a session whose tab is being reaped,
// from a fresh read of the store, since the hook may have written it after
// the tick listed it. A shell record stays: a shell that ended is nothing
// to keep out of the next attach, which opens a new one. A claude or pi
// record goes to the trash, as the sidebar's delete sends it, when the hook
// marked it stopped (the user typed /exit) or a snapshot first showed its
// window reapRan or more before this tick found it gone (/quit, a crash, a
// kill): the row goes, the next attach leaves it alone, and a restore
// brings the conversation back, stopped, for enter to reopen. It stays
// when the window went sooner, or never came, so a launch that fails on
// attach is not thrown away, and when it no longer names the window the
// snapshots showed: a relaunch is under way (Open clears the id before it
// launches) and the record is its to keep. A record already gone from the
// store is nothing to settle.
func (l *Launcher) reapRecord(sess *session.Session, v verdict) error {
	if sess.Kind() == session.AgentShell {
		return nil
	}
	fresh, err := l.store.Load(sess.Name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if fresh.IsActive() && (!v.ran || fresh.KittyWindowID != v.window) {
		return nil
	}
	return l.trash(fresh)
}

// Poke looks for the own agent window now, and again every pokePoll until
// the reap has closed the tab or pokeLooks looks have passed. The exit
// hooks send it (Launcher.Poke) as the agent exits, so the tab goes within
// a moment of the exit rather than up to a poll later. Each look is a List,
// the tick the UI runs, with its reap; the rows it builds are dropped,
// since the UI redraws on its own ticks and the tab is about to close. A
// poke for an agent that stays (a hook fired for a restart) runs out of
// looks and changes nothing; a look that fails (the instance going away)
// ends it. The home sidebar has nothing to look for.
func (b *SidebarBackend) Poke() {
	if b.ownID == "" {
		return
	}
	for i := 0; i < pokeLooks && !b.reap.fired(); i++ {
		if i > 0 {
			b.l.sleep(pokePoll)
		}
		if _, err := b.List(); err != nil {
			return
		}
	}
}

// Poke asks the session's sidebar to look for its agent window now
// (SidebarBackend.Poke) rather than on its next poll, so the tab closes
// within a moment of the agent's exit; the exit hooks send it. The window
// is matched by its id and the session tag together, since ids restart
// with every instance and a stale id alone could name another process,
// which the signal would end. A record without a sidebar id has no sidebar
// to poke.
func (l *Launcher) Poke(sess *session.Session) error {
	if sess.KittySidebarWindowID == 0 || sess.ID == "" {
		return nil
	}
	if err := l.kitty.SignalTagged(sess.KittySidebarWindowID, sess.ID, pokeSignal); err != nil {
		return fmt.Errorf("could not poke the sidebar: %w", err)
	}
	return nil
}
