package launcher

import (
	"fmt"

	"github.com/mad01/kitty-session/internal/kitty"
	"github.com/mad01/kitty-session/internal/repo/config"
	"github.com/mad01/kitty-session/internal/session"
	"github.com/mad01/kitty-session/internal/summary"
)

// backend is the kitty surface the launcher drives. kittyBackend forwards to
// internal/kitty and internal/summary; tests substitute a fake so nothing
// shells out.
type backend interface {
	TabExists(tabID int) bool
	FocusTab(tabID int) error
	FocusWindow(windowID int) error
	LaunchTab(dir string, args ...string) (int, error)
	SetTabTitle(title string) error
	FindTabForWindow(windowID int) (int, error)
	LaunchTabInWindow(windowID int, dir string, args ...string) (int, error)
	LaunchSplit(dir string, args ...string) error
	LaunchSummary(mainWindowID, anyWindowInOSWindow int, dir string) (int, error)
}

// kittyBackend is the production backend.
type kittyBackend struct{}

func (kittyBackend) TabExists(tabID int) bool       { return kitty.TabExists(tabID) }
func (kittyBackend) FocusTab(tabID int) error       { return kitty.FocusTab(tabID) }
func (kittyBackend) FocusWindow(windowID int) error { return kitty.FocusWindow(windowID) }
func (kittyBackend) SetTabTitle(title string) error { return kitty.SetTabTitle(title) }
func (kittyBackend) LaunchSplit(dir string, args ...string) error {
	return kitty.LaunchSplit(dir, args...)
}

func (kittyBackend) LaunchTab(dir string, args ...string) (int, error) {
	return kitty.LaunchTab(dir, args...)
}

func (kittyBackend) FindTabForWindow(windowID int) (int, error) {
	return kitty.FindTabForWindow(windowID)
}

func (kittyBackend) LaunchTabInWindow(windowID int, dir string, args ...string) (int, error) {
	return kitty.LaunchTabInWindow(windowID, dir, args...)
}

func (kittyBackend) LaunchSummary(mainWindowID, anyWindowInOSWindow int, dir string) (int, error) {
	return summary.LaunchTab(mainWindowID, anyWindowInOSWindow, dir)
}

// plan is everything launchTopology needs to lay out one session.
type plan struct {
	name       string
	dir        string
	layout     string // config.LayoutSplit or config.LayoutTab
	summary    bool   // launch the Haiku summary tab
	claudeArgs []string
}

// windows holds the kitty IDs a topology produced plus any non-fatal problems
// met on the way.
type windows struct {
	tabID           int
	claudeWindowID  int
	shellWindowID   int // only for config.LayoutTab
	summaryWindowID int // only when the summary tab launched
	warnings        []error
}

// launchTopology lays out one session in kitty: a new OS window running
// claude, a shell beside it as an hsplit (layout split) or in a sibling tab
// (layout tab), and optionally the Haiku summary tab. It ends with focus on
// the claude window. Summary and focus failures are warnings, not errors.
func launchTopology(b backend, p plan) (windows, error) {
	var w windows
	claudeID, err := b.LaunchTab(p.dir, p.claudeArgs...)
	if err != nil {
		return w, fmt.Errorf("cannot create tab: %w", err)
	}
	w.claudeWindowID = claudeID
	if err := b.SetTabTitle(p.name); err != nil {
		return w, fmt.Errorf("cannot set tab title: %w", err)
	}
	w.tabID, err = b.FindTabForWindow(claudeID)
	if err != nil {
		return w, fmt.Errorf("cannot find tab: %w", err)
	}

	if p.layout == config.LayoutTab {
		w.shellWindowID, err = b.LaunchTabInWindow(claudeID, p.dir)
		if err != nil {
			return w, fmt.Errorf("cannot create shell tab: %w", err)
		}
	} else if err := b.LaunchSplit(p.dir); err != nil {
		return w, fmt.Errorf("cannot create split: %w", err)
	}

	if p.summary {
		id, err := b.LaunchSummary(claudeID, claudeID, p.dir)
		if err != nil {
			w.warnings = append(w.warnings, fmt.Errorf("could not create summary tab: %w", err))
		} else {
			w.summaryWindowID = id
		}
	}
	if err := b.FocusWindow(claudeID); err != nil {
		w.warnings = append(w.warnings, fmt.Errorf("could not focus claude pane: %w", err))
	}
	return w, nil
}

// focus brings a live session to the front: its claude pane when the record
// knows it, the whole tab otherwise.
func focus(b backend, sess *session.Session) error {
	if sess.KittyWindowID != 0 {
		if err := b.FocusWindow(sess.KittyWindowID); err != nil {
			return fmt.Errorf("cannot focus window: %w", err)
		}
		return nil
	}
	if err := b.FocusTab(sess.KittyTabID); err != nil {
		return fmt.Errorf("cannot focus tab: %w", err)
	}
	return nil
}
