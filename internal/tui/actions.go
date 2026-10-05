package tui

import (
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/mad01/kitty-session/internal/claude"
	"github.com/mad01/kitty-session/internal/kitty"
	"github.com/mad01/kitty-session/internal/launcher"
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

// backend is what the TUI needs from the instance: the launcher for every
// session action and liveness, the client for reading claude's terminal.
type backend struct {
	store    *session.Store
	launcher *launcher.Launcher
	kitty    *kitty.Client
}

// detectSessionState determines the Claude state for a session. A record the
// user stopped is stopped whatever kitty shows; otherwise it checks state
// files first (written by hooks or the agent monitor), then falls back to
// terminal text parsing.
func (b backend) detectSessionState(sess *session.Session) claude.State {
	if !sess.IsActive() || !b.launcher.Alive(sess) {
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
			termState := b.readTerminalState(sess)
			if termState == claude.StateIdle || termState == claude.StateNeedsInput {
				return termState
			}
			return claude.StateWorking
		}
	}

	return b.readTerminalState(sess)
}

// readTerminalState reads the claude window's text and classifies the state.
func (b backend) readTerminalState(sess *session.Session) claude.State {
	text, err := b.kitty.GetText(sess.KittyWindowID)
	if err != nil {
		return claude.StateWorking
	}
	return claude.DetectState(text)
}

func (b backend) loadSessions() ([]sessionItem, error) {
	sessions, err := b.store.List()
	if err != nil {
		return nil, err
	}
	items := make([]sessionItem, len(sessions))
	for i, sess := range sessions {
		items[i] = sessionItem{
			session: sess,
			state:   b.detectSessionState(sess),
			context: claude.LatestPrompt(sess.Dir),
		}
	}
	return items, nil
}

func (b backend) openSession(sess *session.Session) error {
	_, err := b.launcher.Open(launcher.Request{Name: sess.Name, Resume: launcher.ResumeStored})
	return err
}

func (b backend) createSession(name, dir string) error {
	_, err := b.launcher.Open(launcher.Request{Name: name, Dir: dir})
	return err
}

// closeSession closes the session's tab but keeps the record, marked stopped
// so a later attach does not treat it as a session to bring back. Tab-close
// warnings have nowhere to go in the TUI and are dropped.
func (b backend) closeSession(sess *session.Session) error {
	_, err := b.launcher.Close(sess, true)
	return err
}

// renameSession renames the record and retitles the tab; a title that cannot
// be set is dropped like other warnings.
func (b backend) renameSession(sess *session.Session, newName string) error {
	_, _, err := b.launcher.Rename(sess.Name, newName)
	return err
}

// deleteSession closes the session's tab and moves the record to the trash,
// where the restore action can bring it back.
func (b backend) deleteSession(sess *session.Session) error {
	_, err := b.launcher.Close(sess, false)
	return err
}

func (b backend) restoreSession(name string) error {
	return b.store.Restore(name)
}

func (b backend) loadTrashedSessions() ([]sessionItem, error) {
	sessions, err := b.store.ListTrashed()
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
