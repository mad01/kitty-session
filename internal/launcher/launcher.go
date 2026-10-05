// Package launcher creates, reopens and closes ks sessions in kitty.
//
// It owns the steps every entry point (ks new, ks open, ks tmp, ks close, the
// TUI) used to carry its own copy of: building the claude command line, laying
// out the session's kitty windows, tearing them down, and recording the result
// in the session store. The kitty layout is confined to launchTopology so it
// can be replaced without touching argument building or store bookkeeping.
package launcher

import (
	"errors"
	"fmt"
	"os"

	"github.com/mad01/kitty-session/internal/claude"
	"github.com/mad01/kitty-session/internal/repo/config"
	"github.com/mad01/kitty-session/internal/session"
)

// ResumeMode selects how claude starts inside the session's window.
type ResumeMode int

const (
	// ResumeNone starts a fresh conversation for a new session record.
	ResumeNone ResumeMode = iota
	// ResumeStored reopens the stored session named in the Request. If its
	// claude window is still alive it is focused; otherwise any leftover
	// tabs are closed and claude is relaunched with --resume <id> when the
	// record carries a Claude session ID whose transcript still exists, and
	// with --continue when it does not.
	ResumeStored
)

// ErrExists is returned by Open when a new session's name is already taken.
// Callers match it with errors.Is to offer a different name.
var ErrExists = errors.New("already exists")

// Request describes the session to open.
type Request struct {
	// Name is the session name. It becomes the kitty tab title and is
	// exported as KS_SESSION_NAME so the ks hook can find the record.
	Name string
	// Dir is the working directory of a new session. ResumeStored ignores
	// it and uses the stored directory.
	Dir string
	// Resume selects how claude starts.
	Resume ResumeMode
}

// Result reports what Open did.
type Result struct {
	// Session is the record as saved, carrying the new kitty IDs unless
	// Focused is set.
	Session *session.Session
	// Focused is true when the stored session's claude window was still
	// alive and was focused instead of relaunched. Nothing was saved then.
	Focused bool
	// Warnings are non-fatal problems (summary tab, focus, or closing a
	// leftover tab). The session is usable regardless.
	Warnings []error
}

// Open creates a new session or focuses/reopens a stored one, then saves the
// record. It drives the real kitty.
func Open(store *session.Store, cfg *config.Config, req Request) (*Result, error) {
	return open(store, cfg, kittyBackend{}, req)
}

func open(store *session.Store, cfg *config.Config, b backend, req Request) (*Result, error) {
	sess, err := target(store, req)
	if err != nil {
		return nil, err
	}
	var warnings []error
	if req.Resume == ResumeStored {
		if claudeAlive(b, sess) {
			if err := focus(b, sess); err != nil {
				return nil, err
			}
			return &Result{Session: sess, Focused: true}, nil
		}
		warnings = closeTabs(b, sess)
	}

	// Save before the first kitty call: claude fires SessionStart as soon as
	// it starts, and that hook must find the record it updates.
	sess.Status = session.StatusActive
	if err := store.Save(sess); err != nil {
		return nil, fmt.Errorf("cannot save session: %w", err)
	}
	w, err := launchTopology(b, plan{
		name:       sess.Name,
		dir:        sess.Dir,
		layout:     cfg.EffectiveLayout(),
		summary:    cfg.SummaryEnabled(),
		claudeArgs: claudeArgs(sess, req.Resume),
	})
	if err != nil {
		return nil, err
	}
	saved, err := recordWindows(store, sess.Name, w)
	if err != nil {
		return nil, err
	}
	return &Result{Session: saved, Warnings: append(warnings, w.warnings...)}, nil
}

// target returns the record to launch: a new one for ResumeNone (ErrExists
// when the name is taken), the stored one for ResumeStored. A stored record
// from before IDs existed gets one now, so the hook can find it by
// KS_SESSION_ID from this launch on.
func target(store *session.Store, req Request) (*session.Session, error) {
	if req.Resume == ResumeNone {
		if store.Exists(req.Name) {
			return nil, fmt.Errorf("session %q %w", req.Name, ErrExists)
		}
		return session.New(req.Name, req.Dir, 0, 0), nil
	}
	sess, err := store.Load(req.Name)
	if err != nil {
		return nil, err
	}
	if sess.ID == "" {
		sess.ID = session.NewID()
	}
	return sess, nil
}

// claudeAlive reports whether the stored session still has its claude window:
// the tab must exist and the claude window must be in it. A record from
// before the window ID was stored can only check the tab.
func claudeAlive(b backend, sess *session.Session) bool {
	if !b.TabExists(sess.KittyTabID) {
		return false
	}
	if sess.KittyWindowID == 0 {
		return true
	}
	return b.WindowExists(sess.KittyWindowID)
}

// claudeArgs builds the kitty launch arguments that start claude for sess.
// PATH is forwarded because kitty @ launch runs with kitty's own environment,
// which may not include the directory claude is installed in.
func claudeArgs(sess *session.Session, mode ResumeMode) []string {
	args := []string{
		"--env", "PATH=" + os.Getenv("PATH"),
		"--env", "KS_SESSION_NAME=" + sess.Name,
		"--env", "KS_SESSION_ID=" + sess.ID,
		"--", "claude",
	}
	if mode == ResumeNone {
		return args
	}
	if sess.ClaudeSessionID != "" && transcriptExists(sess) {
		return append(args, "--resume", sess.ClaudeSessionID)
	}
	return append(args, "--continue")
}

// transcriptExists reports whether Claude Code still has the transcript of
// the session's conversation. Claude Code purges transcripts after its
// cleanup period (30 days by default), after which --resume prints "No
// conversation found" and exits; --continue is the safe fallback then.
func transcriptExists(sess *session.Session) bool {
	path := sess.ClaudeTranscriptPath
	if path == "" {
		derived, err := claude.TranscriptPath(sess.Dir, sess.ClaudeSessionID)
		if err != nil {
			return false
		}
		path = derived
	}
	_, err := os.Stat(path)
	return err == nil
}

// recordWindows reloads the record and applies only the kitty IDs before
// saving, so a SessionStart hook that updated the record while claude was
// starting is not overwritten.
func recordWindows(store *session.Store, name string, w windows) (*session.Session, error) {
	sess, err := store.Load(name)
	if err != nil {
		return nil, fmt.Errorf("cannot reload session after launch: %w", err)
	}
	sess.KittyTabID = w.tabID
	sess.KittyWindowID = w.claudeWindowID
	sess.KittyShellWindowID = w.shellWindowID
	sess.KittySummaryWindowID = w.summaryWindowID
	if err := store.Save(sess); err != nil {
		return nil, fmt.Errorf("cannot save session: %w", err)
	}
	return sess, nil
}
