package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/mad01/kitty-session/internal/claude"
	"github.com/mad01/kitty-session/internal/kitty"
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

// detectSessionState determines the Claude state for a session.
// It uses a hybrid approach: check state files first (written by hooks or
// the agent monitor), then fall back to terminal text parsing.
func detectSessionState(sess *session.Session) claude.State {
	if !kitty.TabExists(sess.KittyTabID) {
		return claude.StateStopped
	}

	// Check state file first (high-confidence, low-latency).
	if s, t, err := state.Read(sess.Name); err == nil {
		if state.IsFresh(t) {
			return mapStringToState(s)
		}
		// State file is stale but said "working" recently — validate
		// against terminal output. If the terminal clearly shows idle
		// or input, use that; otherwise trust "working".
		if mapStringToState(s) == claude.StateWorking && state.IsRecentlyWorking(t) {
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

// mapStringToState converts a state file string to a claude.State.
func mapStringToState(s string) claude.State {
	switch s {
	case "working":
		return claude.StateWorking
	case "idle":
		return claude.StateIdle
	case "input":
		return claude.StateNeedsInput
	case "waiting":
		return claude.StateWaiting
	default:
		return claude.StateUnknown
	}
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
	if kitty.TabExists(sess.KittyTabID) {
		// Focus the Claude pane directly if we have a window ID
		if sess.KittyWindowID != 0 {
			return kitty.FocusWindow(sess.KittyWindowID)
		}
		return kitty.FocusTab(sess.KittyTabID)
	}

	// Recreate the session — pass PATH so claude finds ~/.local/bin
	windowID, err := kitty.LaunchTab(sess.Dir, "--env", "PATH="+os.Getenv("PATH"), "--env", "KS_SESSION_NAME="+sess.Name, "--", "claude")
	if err != nil {
		return fmt.Errorf("cannot create tab: %w", err)
	}
	if err := kitty.SetTabTitle(sess.Name); err != nil {
		return fmt.Errorf("cannot set tab title: %w", err)
	}
	tabID, err := kitty.FindTabForWindow(windowID)
	if err != nil {
		return fmt.Errorf("cannot find tab: %w", err)
	}
	if err := kitty.LaunchSplit(sess.Dir); err != nil {
		return fmt.Errorf("cannot create split: %w", err)
	}
	_ = kitty.FocusWindow(windowID)
	sess.KittyTabID = tabID
	sess.KittyWindowID = windowID
	return store.Save(sess)
}

func createSession(name, dir string, store *session.Store) error {
	windowID, err := kitty.LaunchTab(dir, "--env", "PATH="+os.Getenv("PATH"), "--env", "KS_SESSION_NAME="+name, "--", "claude")
	if err != nil {
		return fmt.Errorf("cannot create tab: %w", err)
	}
	if err := kitty.SetTabTitle(name); err != nil {
		return fmt.Errorf("cannot set tab title: %w", err)
	}
	tabID, err := kitty.FindTabForWindow(windowID)
	if err != nil {
		return fmt.Errorf("cannot find tab: %w", err)
	}
	if err := kitty.LaunchSplit(dir); err != nil {
		return fmt.Errorf("cannot create split: %w", err)
	}
	_ = kitty.FocusWindow(windowID)
	sess := session.New(name, dir, tabID, windowID)
	return store.Save(sess)
}

func closeSession(sess *session.Session) error {
	state.Clean(sess.Name)
	if kitty.TabExists(sess.KittyTabID) {
		return kitty.CloseTab(sess.KittyTabID)
	}
	return nil
}

func renameSession(sess *session.Session, newName string, store *session.Store) error {
	oldName := sess.Name
	if _, err := store.Rename(oldName, newName); err != nil {
		return err
	}
	state.Rename(oldName, newName)
	if kitty.TabExists(sess.KittyTabID) {
		_ = kitty.FocusTab(sess.KittyTabID)
		_ = kitty.SetTabTitle(newName)
	}
	return nil
}

func deleteSession(sess *session.Session, store *session.Store) error {
	state.Clean(sess.Name)
	if kitty.TabExists(sess.KittyTabID) {
		if err := kitty.CloseTab(sess.KittyTabID); err != nil {
			return err
		}
	}
	return store.Delete(sess.Name)
}
