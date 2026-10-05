package tui

import (
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/mad01/kitty-session/internal/claude"
	"github.com/mad01/kitty-session/internal/kitty"
	"github.com/mad01/kitty-session/internal/launcher"
	"github.com/mad01/kitty-session/internal/repo/config"
	"github.com/mad01/kitty-session/internal/session"
	"github.com/mad01/kitty-session/internal/state"
)

// Compile-time interface check.
var _ list.Item = sessionItem{}

// sessionItem implements list.Item for the bubbles/list component.
type sessionItem struct {
	session *session.Session
	state   claude.State
	context string
}

func (i sessionItem) Title() string       { return i.session.Name }
func (i sessionItem) Description() string { return shortenDir(i.session.Dir) }
func (i sessionItem) FilterValue() string { return i.session.Name }

func shortenDir(dir string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return dir
	}
	if strings.HasPrefix(dir, home) {
		return "~" + dir[len(home):]
	}
	return dir
}

// detectSessionState determines the Claude state for a session. A record the
// user stopped is stopped whatever kitty shows; otherwise it checks state
// files first (written by hooks or the agent monitor), then falls back to
// terminal text parsing.
func detectSessionState(sess *session.Session) claude.State {
	if !sess.IsActive() || !kitty.TabExists(sess.KittyTabID) {
		return claude.StateStopped
	}

	// Check state file first (high-confidence, low-latency).
	if s, t, err := state.Read(sess.Name); err == nil {
		if state.IsFresh(t) {
			return claude.ParseState(s)
		}
		// State file is stale but said "working" recently — validate
		// against terminal output. If the terminal clearly shows idle
		// or input, use that; otherwise trust "working".
		if claude.ParseState(s) == claude.StateWorking && state.IsRecentlyWorking(t) {
			termState := readTerminalState(sess)
			if termState == claude.StateIdle || termState == claude.StateNeedsInput {
				return termState
			}
			return claude.StateWorking
		}
	}

	return readTerminalState(sess)
}

// readTerminalState reads the Claude pane text and classifies the state.
func readTerminalState(sess *session.Session) claude.State {
	winID := sess.KittyWindowID
	if winID == 0 {
		id, err := kitty.FirstWindowInTab(sess.KittyTabID)
		if err != nil {
			return claude.StateWorking
		}
		winID = id
	}

	text, err := kitty.GetText(winID)
	if err != nil {
		return claude.StateWorking
	}
	return claude.DetectState(text)
}

func loadSessions(store *session.Store) ([]sessionItem, error) {
	sessions, err := store.List()
	if err != nil {
		return nil, err
	}
	items := make([]sessionItem, len(sessions))
	for i, sess := range sessions {
		items[i] = sessionItem{
			session: sess,
			state:   detectSessionState(sess),
			context: claude.LatestPrompt(sess.Dir),
		}
	}
	return items, nil
}

func openSession(sess *session.Session, store *session.Store) error {
	cfg, _ := config.Load()
	_, err := launcher.Open(store, cfg, launcher.Request{
		Name:   sess.Name,
		Resume: launcher.ResumeStored,
	})
	return err
}

func createSession(name, dir string, store *session.Store) error {
	cfg, _ := config.Load()
	_, err := launcher.Open(store, cfg, launcher.Request{Name: name, Dir: dir})
	return err
}

// closeSession closes the session's kitty tabs but keeps the record, marked
// stopped so a later ks start does not treat it as a session to bring back.
// Tab-close warnings have nowhere to go in the TUI and are dropped.
func closeSession(sess *session.Session, store *session.Store) error {
	_, err := launcher.Close(store, sess, true)
	return err
}

func renameSession(sess *session.Session, newName string, store *session.Store) error {
	oldName := sess.Name
	if _, err := store.Rename(oldName, newName); err != nil {
		return err
	}
	state.Rename(oldName, newName)
	if kitty.TabExists(sess.KittyTabID) {
		winID := sess.KittyWindowID
		if winID == 0 {
			if id, err := kitty.FirstWindowInTab(sess.KittyTabID); err == nil {
				winID = id
			}
		}
		if winID != 0 {
			_ = kitty.SetTabTitleForWindow(newName, winID)
		}
	}
	return nil
}

// deleteSession closes the session's kitty tabs and moves the record to the
// trash, where the restore action can bring it back.
func deleteSession(sess *session.Session, store *session.Store) error {
	_, err := launcher.Close(store, sess, false)
	return err
}

func restoreSession(name string, store *session.Store) error {
	return store.Restore(name)
}

func loadTrashedSessions(store *session.Store) ([]sessionItem, error) {
	sessions, err := store.ListTrashed()
	if err != nil {
		return nil, err
	}
	items := make([]sessionItem, len(sessions))
	for i, sess := range sessions {
		items[i] = sessionItem{
			session: sess,
			state:   claude.StateStopped,
		}
	}
	return items, nil
}
