// Package launcher creates and reopens ks sessions in kitty.
//
// It owns the three steps every entry point (ks new, ks open, ks tmp, the
// TUI) used to carry its own copy of: building the claude command line, laying
// out the session's kitty windows, and recording the resulting IDs in the
// session store. The kitty layout is confined to launchTopology so it can be
// replaced without touching argument building or store bookkeeping.
package launcher

import (
	"fmt"
	"os"

	"github.com/mad01/kitty-session/internal/repo/config"
	"github.com/mad01/kitty-session/internal/session"
)

// ResumeMode selects how claude starts inside the session's window.
type ResumeMode int

const (
	// ResumeNone starts a fresh conversation for a new session record.
	ResumeNone ResumeMode = iota
	// ResumeStored reopens the stored session named in the Request. If its
	// kitty tab is still alive it is focused; otherwise claude is relaunched
	// with --resume <id> when the record carries a Claude session ID, and
	// with --continue when it does not.
	ResumeStored
)

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
	// Focused is true when the stored session's tab was still alive and was
	// focused instead of relaunched. Nothing was saved in that case.
	Focused bool
	// Warnings are non-fatal problems (summary tab or focus failures). The
	// session is usable regardless.
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
	if req.Resume == ResumeStored && b.TabExists(sess.KittyTabID) {
		if err := focus(b, sess); err != nil {
			return nil, err
		}
		return &Result{Session: sess, Focused: true}, nil
	}

	w, err := launchTopology(b, plan{
		name:       req.Name,
		dir:        sess.Dir,
		layout:     cfg.EffectiveLayout(),
		summary:    cfg.SummaryEnabled(),
		claudeArgs: claudeArgs(req.Name, req.Resume, sess.ClaudeSessionID),
	})
	if err != nil {
		return nil, err
	}
	applyWindows(sess, w)
	sess.Status = session.StatusActive
	if err := store.Save(sess); err != nil {
		return nil, fmt.Errorf("cannot save session: %w", err)
	}
	return &Result{Session: sess, Warnings: w.warnings}, nil
}

// target returns the record to launch: a new one for ResumeNone, the stored
// one for ResumeStored.
func target(store *session.Store, req Request) (*session.Session, error) {
	if req.Resume == ResumeNone {
		return session.New(req.Name, req.Dir, 0, 0), nil
	}
	return store.Load(req.Name)
}

// claudeArgs builds the kitty launch arguments that start claude for name.
// PATH is forwarded because kitty @ launch runs with kitty's own environment,
// which may not include the directory claude is installed in.
func claudeArgs(name string, mode ResumeMode, claudeSessionID string) []string {
	args := []string{
		"--env", "PATH=" + os.Getenv("PATH"),
		"--env", "KS_SESSION_NAME=" + name,
		"--", "claude",
	}
	if mode == ResumeNone {
		return args
	}
	if claudeSessionID != "" {
		return append(args, "--resume", claudeSessionID)
	}
	return append(args, "--continue")
}

// applyWindows overwrites every kitty ID on sess with the freshly launched
// ones. Shell and summary IDs become zero when the layout has none.
func applyWindows(sess *session.Session, w windows) {
	sess.KittyTabID = w.tabID
	sess.KittyWindowID = w.claudeWindowID
	sess.KittyShellWindowID = w.shellWindowID
	sess.KittySummaryWindowID = w.summaryWindowID
}
