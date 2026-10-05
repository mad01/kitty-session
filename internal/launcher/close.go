package launcher

import (
	"fmt"

	"github.com/mad01/kitty-session/internal/session"
	"github.com/mad01/kitty-session/internal/state"
)

// Close tears a session down. With keep the record stays, marked stopped, so
// a later ks open can bring the conversation back; without it the record
// moves to the trash. Either way the session's state file is removed and
// every kitty tab it still owns is closed. Tabs that will not close come back
// as warnings; only store failures are errors. It drives the real kitty.
func Close(store *session.Store, sess *session.Session, keep bool) ([]error, error) {
	return closeWith(store, kittyBackend{}, sess, keep)
}

func closeWith(store *session.Store, b backend, sess *session.Session, keep bool) ([]error, error) {
	if keep {
		// Record the stop before the tab goes away: closing the window ends
		// claude with SessionEnd reason "other", which the hook ignores.
		sess.Status = session.StatusStopped
		if err := store.Save(sess); err != nil {
			return nil, fmt.Errorf("cannot save session: %w", err)
		}
	}
	state.Clean(sess.Name)
	warnings := closeTabs(b, sess)
	if keep {
		return warnings, nil
	}
	if err := store.Delete(sess.Name); err != nil {
		return warnings, fmt.Errorf("cannot delete session file: %w", err)
	}
	return warnings, nil
}
