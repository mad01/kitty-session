package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/mad01/kitty-session/internal/claude"
	"github.com/mad01/kitty-session/internal/kitty"
	"github.com/mad01/kitty-session/internal/session"
)

// Compile-time interface check.
var _ list.Item = sessionItem{}

// sessionItem implements list.Item for the bubbles/list component.
type sessionItem struct {
	session *session.Session
	running bool
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

func loadSessions(store *session.Store) ([]sessionItem, error) {
	sessions, err := store.List()
	if err != nil {
		return nil, err
	}
	items := make([]sessionItem, len(sessions))
	for i, sess := range sessions {
		items[i] = sessionItem{
			session: sess,
			running: kitty.TabExists(sess.KittyTabID),
			context: claude.LatestPrompt(sess.Dir),
		}
	}
	return items, nil
}

func openSession(sess *session.Session, store *session.Store) error {
	if kitty.TabExists(sess.KittyTabID) {
		return kitty.FocusTab(sess.KittyTabID)
	}

	// Recreate the session — pass PATH so claude finds ~/.local/bin
	windowID, err := kitty.LaunchTab(sess.Dir, "--env", "PATH="+os.Getenv("PATH"), "--", "claude")
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
	return store.Save(sess)
}

func createSession(name, dir string, store *session.Store) error {
	windowID, err := kitty.LaunchTab(dir, "--env", "PATH="+os.Getenv("PATH"), "--", "claude")
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
	sess := session.New(name, dir, tabID)
	return store.Save(sess)
}

func closeSession(sess *session.Session) error {
	if kitty.TabExists(sess.KittyTabID) {
		return kitty.CloseTab(sess.KittyTabID)
	}
	return nil
}

func deleteSession(sess *session.Session, store *session.Store) error {
	if kitty.TabExists(sess.KittyTabID) {
		if err := kitty.CloseTab(sess.KittyTabID); err != nil {
			return err
		}
	}
	return store.Delete(sess.Name)
}
