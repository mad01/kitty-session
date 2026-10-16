package launcher

import (
	"fmt"
	"slices"
	"time"

	"github.com/mad01/kitty-session/internal/kitty"
	"github.com/mad01/kitty-session/internal/session"
)

const (
	// closeSignal tells a session's sidebar to take its desktop notification
	// down and close its own tab; every ks sidebar handles it (internal/cli).
	// It is SIGUSR1, whose default action would otherwise end the process.
	closeSignal = "SIGUSR1"
	// pokeSignal tells a session's sidebar to look for its agent window now
	// (SidebarBackend.Poke); the exit hooks send it through Launcher.Poke.
	// It is SIGUSR2, handled by every ks sidebar like closeSignal.
	pokeSignal = "SIGUSR2"
	// closeGrace caps how long the launcher waits for the signalled sidebars
	// to close their tabs before it closes whatever they left behind.
	closeGrace = 500 * time.Millisecond
	// closePoll is how often awaitClosed re-reads the instance while it waits.
	closePoll = 50 * time.Millisecond
)

// awaitClosed polls the instance until no window carries any of the session
// ids, or closeGrace elapses, and returns the last snapshot it read. prev
// seeds the return value so a first poll that fails still hands back a
// usable snapshot. A poll error keeps the previous snapshot and never
// aborts, so a transient ls failure does not cut the wait short. A sidebar
// that handled the signal has closed its tab, so its windows are gone; an
// older build that died on the signal leaves its claude window, which the
// caller then closes.
func (l *Launcher) awaitClosed(prev []kitty.Window, ids []string) []kitty.Window {
	polls := int(closeGrace / closePoll)
	all := prev
	for i := 0; ; i++ {
		if next, err := l.kitty.Windows(); err == nil {
			all = next
		}
		if i >= polls || noneTagged(all, ids) {
			return all
		}
		l.sleep(closePoll)
	}
}

// noneTagged reports whether no window in the snapshot carries any of the
// session ids.
func noneTagged(all []kitty.Window, ids []string) bool {
	return !slices.ContainsFunc(all, func(w kitty.Window) bool {
		return w.SessionID != "" && slices.Contains(ids, w.SessionID)
	})
}

// WithdrawBanners asks every session's live sidebar, except the one whose id
// is exceptID, to take its banner down and close its own tab (closeSignal),
// then waits once for those tabs to go (awaitClosed). Quit runs it before
// closing the instance: a sidebar's notification would otherwise outlive it.
// exceptID is the caller's own session, which withdraws its banner inline
// rather than signalling itself. CloseAll then takes whatever is left,
// including any home tab a closing sidebar recreated. A failed signal, and
// an instance that cannot be listed, come back as warnings.
func (l *Launcher) WithdrawBanners(sessions []*session.Session, exceptID string) []error {
	all, err := l.kitty.Windows()
	if err != nil {
		return []error{fmt.Errorf("notifications left as they are: %w", err)}
	}
	var warnings []error
	var ids []string
	for _, sess := range sessions {
		if sess.ID == exceptID {
			continue
		}
		lv := findLive(all, sess)
		if lv.sidebar == nil {
			continue
		}
		if err := l.kitty.SignalChild(lv.sidebar.ID, closeSignal); err != nil {
			warnings = append(warnings, fmt.Errorf("%s: %w", sess.Name, err))
			continue
		}
		ids = append(ids, sess.ID)
	}
	if len(ids) > 0 {
		l.awaitClosed(all, ids)
	}
	return warnings
}
