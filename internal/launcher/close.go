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
	if keep {
		// Record the stop before the tab goes away: closing the window ends
		// claude with SessionEnd reason "other", which the hook ignores.
		sess.Status = session.StatusStopped
		if err := l.store.Save(sess); err != nil {
			return nil, fmt.Errorf("cannot save session: %w", err)
		}
	}
	state.Clean(sess.Name)
	var warnings []error
	lv, err := l.liveWindows(sess)
	if err != nil {
		warnings = append(warnings, fmt.Errorf("tabs left as they are: %w", err))
	} else {
		warnings = l.closeTabs(lv)
	}
	if keep {
		return warnings, nil
	}
	if err := l.store.Delete(sess.Name); err != nil {
		return warnings, fmt.Errorf("cannot delete session file: %w", err)
	}
	return warnings, nil
}
