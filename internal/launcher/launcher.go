// Package launcher creates, reopens, renames and closes ks sessions in the ks
// kitty instance, and attaches to the instance as a whole.
//
// It owns the steps every entry point (ks, ks new, ks open, ks tmp, ks close,
// ks rename, the sidebar TUI) used to carry its own copy of: building the
// claude command line, laying out the session's tab, telling the session's
// windows apart from everything else in the instance, tearing them down, and
// recording the result in the session store. The kitty layout is confined to
// topology.go so it can change without touching argument building or store
// bookkeeping.
package launcher

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/mad01/kitty-session/internal/claude"
	"github.com/mad01/kitty-session/internal/kitty"
	"github.com/mad01/kitty-session/internal/repo/config"
	"github.com/mad01/kitty-session/internal/session"
)

// ResumeMode selects how claude starts inside the session's window.
type ResumeMode int

const (
	// ResumeNone starts a fresh conversation for a new session record.
	ResumeNone ResumeMode = iota
	// ResumeStored reopens the stored session named in the Request. If its
	// claude window is still alive it is focused. If only its sidebar is
	// left, claude is relaunched beside it. Otherwise any leftover tab is
	// closed and the whole tab is recreated. A relaunch uses --resume <id>
	// when the record carries a Claude session ID whose transcript still
	// exists, and --continue when it does not.
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
	// Background leaves a newly built tab hidden instead of showing it.
	// Attach uses it to bring every session back and then focus one.
	Background bool
}

// Result reports what Open did.
type Result struct {
	// Session is the record as saved.
	Session *session.Session
	// Focused is true when the stored session's claude window was still
	// alive and was focused instead of relaunched.
	Focused bool
	// Warnings are non-fatal problems (geometry, focus, or closing a
	// leftover tab). The session is usable regardless.
	Warnings []error
}

// Launcher drives one kitty instance on behalf of one session store.
type Launcher struct {
	store        *session.Store
	kitty        backend
	sidebarWidth int
	exe          string // the ks binary each tab's sidebar runs
	now          func() time.Time
	sleep        func(time.Duration)
}

// New returns a launcher for the instance behind client. cfg may be nil.
func New(store *session.Store, client *kitty.Client, cfg *config.Config) (*Launcher, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("cannot locate the ks binary: %w", err)
	}
	return newLauncher(store, client, cfg.EffectiveSidebarWidth(), exe), nil
}

func newLauncher(store *session.Store, b backend, sidebarWidth int, exe string) *Launcher {
	return &Launcher{
		store:        store,
		kitty:        b,
		sidebarWidth: sidebarWidth,
		exe:          exe,
		now:          time.Now,
		sleep:        time.Sleep,
	}
}

// Open creates a new session or focuses/reopens a stored one, then saves the
// record. A tab is built hidden and shown with one focus switch at the end,
// which req.Background skips. Either way the home tab is retired afterwards:
// a session tab now exists to hold the instance up.
func (l *Launcher) Open(req Request) (*Result, error) {
	sess, err := l.target(req)
	if err != nil {
		return nil, err
	}
	var (
		warnings []error
		sidebar  *kitty.Window // the stored session's surviving sidebar, if any
	)
	if req.Resume == ResumeStored {
		all, lv, err := l.snapshot(sess)
		if err != nil {
			return nil, fmt.Errorf("cannot list kitty windows: %w", err)
		}
		if lv.claude != nil {
			return l.focus(sess, lv.claude.ID, l.retireHomeIn(all))
		}
		sidebar = lv.sidebar
		if sidebar == nil {
			warnings = l.closeTabs(all, lv)
		}
	}

	// Save before the first kitty call: claude fires SessionStart as soon as
	// it starts, and that hook must find the record it updates.
	sess.Status = session.StatusActive
	if err := l.store.Save(sess); err != nil {
		return nil, fmt.Errorf("cannot save session: %w", err)
	}
	w, err := l.launch(sess, req.Resume, sidebar)
	if err != nil {
		return nil, err
	}
	saved, err := l.recordWindows(sess.Name, w)
	if err != nil {
		return nil, err
	}
	warnings = append(warnings, w.warnings...)
	if !req.Background {
		warnings = append(warnings, l.show(w.claudeID)...)
	}
	warnings = append(warnings, l.retireHome()...)
	return &Result{Session: saved, Warnings: warnings}, nil
}

// show brings a freshly built tab to the front by focusing its claude window:
// the one switch the user sees, after the layout has settled out of sight.
func (l *Launcher) show(claudeID int) []error {
	if err := l.kitty.FocusWindow(claudeID); err != nil {
		return []error{fmt.Errorf("could not focus claude window: %w", err)}
	}
	return nil
}

// launch lays the session out: claude beside a surviving sidebar, or a
// whole new tab.
func (l *Launcher) launch(
	sess *session.Session,
	mode ResumeMode,
	sidebar *kitty.Window,
) (windows, error) {
	p := l.plan(sess, mode)
	if sidebar != nil {
		return l.relaunchClaude(p, *sidebar)
	}
	return l.launchTopology(p)
}

// target returns the record to launch: a new one for ResumeNone (ErrExists
// when the name is taken), the stored one for ResumeStored. A stored record
// from before IDs existed gets one now, so the hook can find it by
// KS_SESSION_ID from this launch on.
func (l *Launcher) target(req Request) (*session.Session, error) {
	if req.Resume == ResumeNone {
		if l.store.Exists(req.Name) {
			return nil, fmt.Errorf("session %q %w", req.Name, ErrExists)
		}
		return session.New(req.Name, req.Dir, 0, 0), nil
	}
	sess, err := l.store.Load(req.Name)
	if err != nil {
		return nil, err
	}
	if sess.ID == "" {
		sess.ID = session.NewID()
	}
	return sess, nil
}

// Alive reports whether the session's claude window is in the instance. An
// unreachable instance counts as no.
func (l *Launcher) Alive(sess *session.Session) bool {
	lv, err := l.liveWindows(sess)
	return err == nil && lv.claude != nil
}

// aliveIn reports whether sess has a claude window in an already-taken
// snapshot, so a scan over many sessions costs one Windows() call, not one
// per session.
func (l *Launcher) aliveIn(all []kitty.Window, sess *session.Session) bool {
	return findLive(all, sess).claude != nil
}

// focus brings a live session to the front and stamps FocusedAt, carrying
// the caller's warnings into the result.
func (l *Launcher) focus(
	sess *session.Session,
	claudeWindow int,
	warnings []error,
) (*Result, error) {
	if err := l.kitty.FocusWindow(claudeWindow); err != nil {
		return nil, fmt.Errorf("cannot focus window: %w", err)
	}
	saved, err := l.stampFocus(sess.Name)
	if err != nil {
		return nil, err
	}
	return &Result{Session: saved, Focused: true, Warnings: warnings}, nil
}

// stampFocus reloads the record and sets FocusedAt, so a hook update made in
// the meantime is kept.
func (l *Launcher) stampFocus(name string) (*session.Session, error) {
	sess, err := l.store.Load(name)
	if err != nil {
		return nil, fmt.Errorf("cannot reload session: %w", err)
	}
	sess.FocusedAt = l.now()
	if err := l.store.Save(sess); err != nil {
		return nil, fmt.Errorf("cannot save session: %w", err)
	}
	return sess, nil
}

// plan gathers what the topology needs to lay out sess.
func (l *Launcher) plan(sess *session.Session, mode ResumeMode) plan {
	return plan{
		name:       sess.Name,
		dir:        sess.Dir,
		env:        sessionEnv(sess),
		vars:       []string{kitty.SessionVar + "=" + sess.ID},
		sidebarCmd: []string{l.exe, "sidebar", "--session-id", sess.ID},
		claudeCmd:  ClaudeCmd(sess, mode),
	}
}

// sessionEnv is exported into both of the session's windows. PATH is
// forwarded because kitty @ launch runs with kitty's own environment, which
// may not include the directory claude is installed in; the two KS variables
// let the ks hook find the record.
func sessionEnv(sess *session.Session) []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"KS_SESSION_NAME=" + sess.Name,
		"KS_SESSION_ID=" + sess.ID,
	}
}

// ClaudeCmd builds the command that starts claude for sess: --resume <id>
// when the record's own transcript still exists, --continue when the
// directory has any transcript to continue, and a fresh claude otherwise.
// --continue with nothing to continue makes claude exit at once.
func ClaudeCmd(sess *session.Session, mode ResumeMode) []string {
	cmd := []string{"claude"}
	if mode == ResumeNone {
		return cmd
	}
	if sess.ClaudeSessionID != "" && transcriptExists(sess) {
		return append(cmd, "--resume", sess.ClaudeSessionID)
	}
	if claude.HasTranscripts(sess.Dir) {
		return append(cmd, "--continue")
	}
	return cmd
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

// recordWindows reloads the record and applies only the kitty IDs and the
// focus stamp before saving, so a SessionStart hook that updated the record
// while claude was starting is not overwritten. The pre-instance shell and
// summary IDs are cleared: this topology has neither.
func (l *Launcher) recordWindows(name string, w windows) (*session.Session, error) {
	sess, err := l.store.Load(name)
	if err != nil {
		return nil, fmt.Errorf("cannot reload session after launch: %w", err)
	}
	sess.KittyTabID = w.tabID
	sess.KittyWindowID = w.claudeID
	sess.KittySidebarWindowID = w.sidebarID
	sess.KittyShellWindowID = 0
	sess.KittySummaryWindowID = 0
	sess.FocusedAt = l.now()
	if err := l.store.Save(sess); err != nil {
		return nil, fmt.Errorf("cannot save session: %w", err)
	}
	return sess, nil
}
