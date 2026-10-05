package launcher

import (
	"fmt"

	"github.com/mad01/kitty-session/internal/session"
	"github.com/mad01/kitty-session/internal/state"
)

// Close tears a session down. With keep the record stays, marked stopped, so
// a later ks open can bring the conversation back; without it the record
// moves to the trash. Either way the session's state file is removed and
// every tab it still owns is closed. Tabs that will not close, or an
// instance that cannot be reached, come back as warnings; only store
// failures are errors.
func (l *Launcher) Close(sess *session.Session, keep bool) ([]error, error) {
	// Every store mutation happens before the first kitty call. The sidebar
	// that issued this close lives in the tab being closed and is killed by
	// SIGHUP the instant the tab goes, so a store write left until after the
	// close might never run. The in-memory sess keeps its kitty ids, so the
	// tab can still be found after the record is trashed.
	if keep {
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

	lv, err := l.liveWindows(sess)
	if err != nil {
		return []error{fmt.Errorf("tabs left as they are: %w", err)}, nil
	}
	return l.closeTabs(lv), nil
}
