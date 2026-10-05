package launcher

import (
	"fmt"

	"github.com/mad01/kitty-session/internal/session"
	"github.com/mad01/kitty-session/internal/state"
)

// Rename gives the session a new name: the record, its state file and, when
// the session has a live tab, the tab title. A title that cannot be set is a
// warning; the rename itself has already happened.
func (l *Launcher) Rename(oldName, newName string) (*session.Session, []error, error) {
	sess, err := l.store.Rename(oldName, newName)
	if err != nil {
		return nil, nil, err
	}
	state.Rename(oldName, newName)
	lv, err := l.liveWindows(sess)
	if err != nil {
		return sess, []error{fmt.Errorf("tab title not updated: %w", err)}, nil
	}
	anchor := lv.sidebar
	if anchor == nil {
		anchor = lv.claude
	}
	if anchor == nil {
		return sess, nil, nil // no live tab to retitle
	}
	if err := l.kitty.SetTabTitleForWindow(newName, anchor.ID); err != nil {
		return sess, []error{fmt.Errorf("tab title not updated: %w", err)}, nil
	}
	return sess, nil, nil
}
