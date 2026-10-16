package launcher

import (
	"fmt"

	"github.com/mad01/kitty-session/internal/session"
	"github.com/mad01/kitty-session/internal/state"
)

// CloseOptions tunes Close.
type CloseOptions struct {
	// Keep leaves the record, marked stopped, instead of trashing it.
	Keep bool
	// Signal asks the session's sidebar to take its banner down and close
	// its own tab before Close touches the tabs. The caller sets it false
	// when it is that sidebar itself: it has already withdrawn its banner,
	// and it cannot answer a signal while it is blocked in this call. A
	// close from a shell or from another session's sidebar sets it true.
	Signal bool
}

// Close tears a session down. With Keep the record stays, marked stopped, so
// a later ks open can bring the conversation back; without it the record
// moves to the trash. Either way the session's state file is removed. With
// Signal and a live sidebar, that sidebar is asked to take its banner down
// and close its own tab, and Close waits up to closeGrace for the tab to go
// (awaitClosed); it then closes whatever is left from the post-wait
// snapshot, so a tab the sidebar already closed is not closed again and an
// older build that died on the signal has its leftover window closed. A
// sidebar that cannot be signalled, a tab that will not close, or an
// instance that cannot be reached, come back as warnings; only store
// failures are errors.
func (l *Launcher) Close(sess *session.Session, opts CloseOptions) ([]error, error) {
	// Every store mutation happens before the first kitty call. The sidebar
	// that issued this close lives in the tab being closed and is killed by
	// SIGHUP the instant the tab goes, so a store write left until after the
	// close might never run. The in-memory sess keeps its kitty ids, so the
	// tab can still be found after the record is trashed.
	if opts.Keep {
		// Record the stop before the tab goes away: closing the window ends
		// claude with SessionEnd reason "other", which the hook ignores.
		sess.Status = session.StatusStopped
		if err := l.store.Save(sess); err != nil {
			return nil, fmt.Errorf("cannot save session: %w", err)
		}
	} else if err := l.store.Delete(sess.Name); err != nil {
		return nil, fmt.Errorf("cannot delete session file: %w", err)
	}
	state.Clean(sess.Name)

	all, lv, err := l.snapshot(sess)
	if err != nil {
		return []error{fmt.Errorf("tabs left as they are: %w", err)}, nil
	}
	var warnings []error
	if opts.Signal && lv.sidebar != nil {
		if err := l.kitty.SignalChild(lv.sidebar.ID, closeSignal); err != nil {
			warnings = append(warnings, fmt.Errorf("could not signal the sidebar: %w", err))
		} else {
			all = l.awaitClosed(all, []string{sess.ID})
			lv = findLive(all, sess)
		}
	}
	return append(warnings, l.closeTabs(all, lv)...), nil
}
