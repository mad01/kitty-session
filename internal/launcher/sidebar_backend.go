package launcher

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/mad01/kitty-session/internal/claude"
	"github.com/mad01/kitty-session/internal/hooks"
	"github.com/mad01/kitty-session/internal/instance"
	"github.com/mad01/kitty-session/internal/kitty"
	"github.com/mad01/kitty-session/internal/repo/config"
	"github.com/mad01/kitty-session/internal/repo/finder"
	"github.com/mad01/kitty-session/internal/session"
	"github.com/mad01/kitty-session/internal/sidebar"
	"github.com/mad01/kitty-session/internal/state"
)

const (
	// viewedDebounce is the least time between two ViewedAt stamps, so a
	// sidebar whose tab stays in front does not rewrite its record on every
	// poll.
	viewedDebounce = 10 * time.Second
	// shellSplitBias is the share of claude's height a shell split takes.
	shellSplitBias = 30
	// raiseDebounce is the least time between two reads of the own window's
	// text that look for a dialog (backstop's raise), which goes on for as
	// long as no hook has written a state file, as at a login prompt.
	raiseDebounce = 10 * time.Second
)

// SidebarBackend is the sidebar.Backend over one ks instance: every action
// of the sidebar UI mapped onto the launcher, the store and the instance.
// One runs in every sidebar process; ownID is the ID of the session whose
// tab the sidebar sits in, empty in the home tab. Identity is the ID, not the
// name, so a rename shows up on the next List without restarting the sidebar.
type SidebarBackend struct {
	l     *Launcher
	cfg   *config.Config
	ownID string
	home  string // for shortening directories in titles
	quit  func() error
	// readState reads a session's state file; tests substitute a fake.
	readState func(name string) (state.Entry, error)
	// writeState writes a session's state file, for backstop; tests
	// substitute a fake.
	writeState func(name string, e state.Entry) error
	// readText reads the live bottom of a window's terminal text, for
	// backstop; tests substitute a fake.
	readText func(windowID int) (string, error)

	// withdraw takes down the own session's desktop notification. It runs
	// before the sidebar closes its own tab (reapIfGone), and on the SIGUSR1
	// a launcher close sends (WithdrawAndClose), since the sidebar window,
	// which posted the notification, goes with the tab. Nil when there is
	// none to take down.
	withdraw func()

	mu         sync.Mutex // guards the fields below; List runs off the UI loop
	lastViewed time.Time
	// ownVisible is whether the own session's tab was visible (live.visible)
	// at the previous poll, so the polls that find it newly visible, or no
	// longer so, can stamp ViewedAt at once instead of waiting out
	// viewedDebounce.
	ownVisible bool
	lastRaise  time.Time // when backstop last read the text for a dialog
	reap       reaper    // closes the own tab once its claude is gone; see reapIfGone
}

// NewSidebarBackend returns the backend for the instance behind client.
// cfg may be nil; ownID is the id of the session whose tab this sidebar sits
// in (empty in the home tab); withdraw, which may be nil, takes down the
// session's desktop notification and runs before the sidebar closes its own
// tab.
func NewSidebarBackend(
	store *session.Store,
	client *kitty.Client,
	cfg *config.Config,
	ownID string,
	withdraw func(),
) (*SidebarBackend, error) {
	l, err := New(store, client, cfg)
	if err != nil {
		return nil, err
	}
	b := newSidebarBackend(l, cfg, ownID)
	b.quit = func() error { return instance.Shutdown(client) }
	b.readText = client.GetTextAll
	b.withdraw = withdraw
	return b, nil
}

func newSidebarBackend(l *Launcher, cfg *config.Config, ownID string) *SidebarBackend {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	return &SidebarBackend{
		l:          l,
		cfg:        cfg,
		ownID:      ownID,
		home:       home,
		quit:       func() error { return errors.New("launcher: quit is not wired") },
		readState:  state.ReadEntry,
		writeState: state.WriteEntry,
		readText: func(int) (string, error) {
			return "", errors.New("launcher: text reader is not wired")
		},
	}
}

// List returns one agent per session record from one store listing and one
// instance snapshot. Windows are matched to records by their session tag.
// The own session's record is stamped as viewed before its state is
// resolved while its tab is visible, in front of a focused OS window
// (markViewed). Once the own session's claude window is gone, its tab is
// closed from the same snapshot (reapIfGone).
func (b *SidebarBackend) List() ([]sidebar.Agent, error) {
	sessions, err := b.l.store.List()
	if err != nil {
		return nil, err
	}
	all, err := b.l.kitty.Windows()
	if err != nil {
		return nil, fmt.Errorf("cannot list kitty windows: %w", err)
	}
	pos := tabPositions(all)
	agents := make([]sidebar.Agent, 0, len(sessions))
	for _, sess := range sessions {
		lv := findLive(all, sess)
		own := b.ownID != "" && sess.ID == b.ownID
		if own {
			if sess, err = b.markViewed(sess, lv.visible(b.l.now())); err != nil {
				return nil, err
			}
		}
		agents = append(agents, b.agent(sess, lv, own, pos[lv.tabID()]))
		if own {
			b.reapIfGone(all, lv, sess)
		}
	}
	return agents, nil
}

// markViewed records whether the own tab is visible and stamps ViewedAt on
// a fresh copy of the record, so a hook update made in the meantime is
// kept, at three moments: the poll that finds the tab newly visible (the
// user has just looked), every viewedDebounce while it stays visible, and
// the poll that finds it no longer visible (the user watched until the
// switch). The brief moment ks move brings a tab to the front to reorder it
// does not count: Move marks the sidebar window KS_MOVING, which live.visible
// reads as not seen. It returns the record to resolve state from.
func (b *SidebarBackend) markViewed(
	sess *session.Session, visible bool,
) (*session.Session, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	was := b.ownVisible
	b.ownVisible = visible
	now := b.l.now()
	switch {
	case !visible && !was:
		return sess, nil
	case visible && was && now.Sub(b.lastViewed) < viewedDebounce:
		return sess, nil
	}
	fresh, err := b.l.store.Load(sess.Name)
	if errors.Is(err, fs.ErrNotExist) {
		// Trashed under this tick, by the reap of an overlapping one or a
		// ks close: nothing to stamp, and the tab is on its way out.
		return sess, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot reload session: %w", err)
	}
	fresh.ViewedAt = now
	if err := b.l.store.Save(fresh); err != nil {
		return nil, fmt.Errorf("cannot save session: %w", err)
	}
	b.lastViewed = now
	return fresh, nil
}

// agent builds the sidebar row for sess from its live windows; tab is the
// position of the session's tab in kitty's order, zero without one.
func (b *SidebarBackend) agent(sess *session.Session, lv live, own bool, tab int) sidebar.Agent {
	w := lv.claude
	in := stateInput{
		kind: sess.Kind(), active: sess.IsActive(), viewedAt: sess.ViewedAt,
		visible: lv.visible(b.l.now()),
	}
	if w != nil {
		in.hasWindow, in.title, in.report = true, w.Title, w.Status
	}
	if e, err := b.readState(sess.Name); err == nil {
		in.fileState, in.fileAt, in.fileSource = e.State, e.UpdatedAt, e.Source
	}
	st := resolveState(in)
	if own {
		st = b.backstop(sess.Name, w, in, st)
	}
	return sidebar.Agent{
		Name:    sess.Name,
		Kind:    in.kind,
		Dir:     sess.Dir,
		Title:   b.title(in, sess.Dir),
		State:   st,
		Waiting: b.waiting(st, in.fileState, in.fileAt),
		Tab:     tab,
		Own:     own,
		Unread:  unread(in, st),
	}
}

// unread reports whether the session changed since the user last looked at
// it: its state file, which the hooks, the screen backstop and the pi
// extension all write, is newer than the record's ViewedAt, and the tab is
// not visible (in front of a focused OS window). Any state counts, a
// running turn included, the way kitty's own tab activity marker does,
// except waiting: the SessionStart hook writes it at launch, and a launch
// is not activity. A stopped row, or one without a state file, is never
// unread.
func unread(in stateInput, st sidebar.State) bool {
	if st == sidebar.StateStopped || in.fileAt.IsZero() || in.visible {
		return false
	}
	if claude.ParseState(in.fileState) == claude.StateWaiting {
		return false
	}
	return in.fileAt.After(in.viewedAt)
}

// waiting is how long an input row has been waiting on the user: the age of
// the state file that reported the prompt, so only when the file itself
// says input. Every other row reports zero, an input row the record alone
// made included: a Program Status record carries no time, so without hooks
// the age reads zero even when the record says blocked.
func (b *SidebarBackend) waiting(
	st sidebar.State, fileState string, fileAt time.Time,
) time.Duration {
	if st != sidebar.StateInput || claude.ParseState(fileState) != claude.StateNeedsInput {
		return 0
	}
	if fileAt.IsZero() {
		return 0
	}
	return max(b.l.now().Sub(fileAt), 0)
}

// stateInput is everything resolveState looks at for one session.
type stateInput struct {
	kind      string // the session's agent kind, as session.Kind reports it
	active    bool   // the record is not stopped
	hasWindow bool   // the agent window is in the instance
	title     string // the agent window's title
	fileState string // the state file's state, "" without a file
	fileAt    time.Time
	// fileSource is the state file's writer: state.SourceScreen for the
	// backstop's own correction, "" for a hook or the pi extension.
	fileSource string
	viewedAt   time.Time
	visible    bool // the user is looking at the session's tab: live.visible
	// report is the state of the agent window's Program Status record, ""
	// without one.
	report kitty.ProgramState
}

// resolveState picks the sidebar state for one session. Whatever the kind,
// a stopped record or a missing agent window is stopped. Two rules then
// apply whatever the kind, in this order:
//
//	state file fresh (state.IsFresh)                → the kind's own rules below: the hooks saw the event
//	agent window has a Program Status record        → blocked is input; working, done, error and idle are themselves
//
// The record (OSC 7501, mapped by ProgramState and gated by ReportedState)
// is the program's own word, so a shell running a program that speaks the
// protocol reports through it too. Without a record, or with a fresh state
// file, a shell session is idle, since nothing else reports its state, and
// for claude, first match wins:
//
//	state file fresh and input                      → input
//	state file fresh and working                    → working
//	state file fresh and error                      → error
//	title glyph working                             → working
//	state file input, whatever its age              → input
//	state file error, whatever its age              → error
//	title glyph idle (✳)                            → done if the state file says idle later than viewed_at, else idle
//	no glyph: state file working                    → working
//	anything else (idle, waiting, no state file)    → idle
//
// A pi session has no title glyph, so without a record, or with a fresh
// state file, only the state file, written by the ks pi extension, speaks:
//
//	state file input, whatever its age              → input
//	state file working, whatever its age            → working
//	state file idle later than viewed_at            → done
//	anything else (idle, waiting, no state file)    → idle
//
// Whatever produced it, a done on the tab the user is looking at reads idle:
// the result is already on screen. kitty drops its own done record on a
// focus-in or a keypress, never on focus-out, so a turn that ends while the
// user sits in the window would otherwise stay done; the state-file done the
// hooks and the pi extension produce is turned the same way. resolveState
// applies that once, after the rules above.
//
// A fresh working state file outranks the ✳ idle title because ✳ is also one
// of Claude Code's spinner frames, so a mid-turn snapshot can catch it while
// the hooks already know the turn is still running.
//
// An input state file is sticky. The hooks write it when Claude is about to
// show a permission prompt or a question, and nothing replaces it until the
// next event they report: a prompt or tool call (working), Stop (idle),
// StopFailure (error) or SessionEnd (file removed). So a prompt left
// unanswered keeps its row at input however old the file is. The one signal
// that outranks a stale input is a working title glyph: the user has
// answered and Claude is mid-turn, which no hook reports before its next
// tool call. A fresh input still beats that glyph, since the title can lag
// the hooks when the prompt appears. An error state file, written by the
// StopFailure hook when a turn dies on an API error, is sticky the same way
// and ranks right under input: the failure needs the user as much as a
// prompt does, and only a retry (working) or a later event clears it.
//
// The own session's sidebar runs one check more on its claude row after
// these rules, backstop: when the hooks left a stale input, or no state at
// all, or a stale waiting, and no record speaks, it reads the window's
// text and corrects the state from what is on screen, writing the
// correction to the state file. It is the single writer of that
// correction, so every other sidebar, which never reads the text, and ks
// list follow through the file.
func resolveState(in stateInput) sidebar.State {
	if !in.active || !in.hasWindow {
		return sidebar.StateStopped
	}
	st := resolveActive(in)
	// A tab the user is looking at never reads done: the result is on screen.
	if st == sidebar.StateDone && in.visible {
		return sidebar.StateIdle
	}
	return st
}

// resolveActive picks a live session's state by the rules in resolveState's
// comment, before the visible-tab done→idle pass resolveState applies.
func resolveActive(in stateInput) sidebar.State {
	if st, ok := ReportedState(in.report, in.fileAt); ok {
		return st
	}
	switch in.kind {
	case session.AgentShell:
		return sidebar.StateIdle
	case session.AgentPi:
		return fileOnlyState(in)
	default:
		return claudeState(in)
	}
}

// claudeState is resolveState's claude branch: the state file and the title
// glyph arbitrated as the doc comment's table says.
func claudeState(in stateInput) sidebar.State {
	fileState := claude.ParseState(in.fileState)
	if state.IsFresh(in.fileAt) {
		switch fileState {
		case claude.StateNeedsInput:
			return sidebar.StateInput
		case claude.StateWorking:
			return sidebar.StateWorking
		case claude.StateError:
			return sidebar.StateError
		}
	}
	titleState, _, hasGlyph := claude.ParseTitle(in.title)
	if hasGlyph && titleState == claude.StateWorking {
		return sidebar.StateWorking
	}
	switch fileState {
	case claude.StateNeedsInput:
		return sidebar.StateInput
	case claude.StateError:
		return sidebar.StateError
	}
	if hasGlyph { // the idle glyph ✳
		if fileState == claude.StateIdle && in.fileAt.After(in.viewedAt) {
			return sidebar.StateDone
		}
		return sidebar.StateIdle
	}
	if fileState == claude.StateWorking {
		return sidebar.StateWorking
	}
	return sidebar.StateIdle
}

// backstop is the screen check the own sidebar runs on its claude row after
// resolveState, for the two gaps the hooks leave when no Program Status
// record speaks. A stale input (the hooks said prompt, and nothing since)
// is cleared when the window shows a running turn or the prompt box at
// rest: the user answered through a path no hook reports. A dialog still
// on screen keeps it, whatever else the screen shows, and a running turn
// outranks a drawn prompt box, since Claude Code keeps the box on screen
// mid-turn. An input is raised when the window shows a dialog and the
// raise gate is open (raiseDue): no state file or a stale waiting, as at
// startup, where the folder-trust dialog and a login prompt come before
// any hook fires, or a file the screen check itself wrote last (its
// Source). Every claude ks launches carries the hooks (they are injected
// with --settings), so the first hook write hands the raise to the hooks;
// a claude whose injected hooks never fire (disableAllHooks, a build that
// ignores them) never makes that write, and the screen check keeps the
// gate for the whole session. A clear reads the text on every poll, since
// that wait is the user's; a raise reads at most once per raiseDebounce.
// The text is cut to
// the window's rows (claude.LiveScreen) so that what Ink left in the
// scrollback above a short screen does not count. A read error leaves the
// row alone. The corrected state is written to the state file (see
// resolveState and correct) and shown on the row.
func (b *SidebarBackend) backstop(
	name string, w *kitty.Window, in stateInput, st sidebar.State,
) sidebar.State {
	if in.kind != session.AgentClaude || in.report != "" || w == nil ||
		st == sidebar.StateStopped {
		return st
	}
	clear := st == sidebar.StateInput && !state.IsFresh(in.fileAt)
	if !clear && !b.raiseDue(in, st) {
		return st
	}
	text, err := b.readText(w.ID)
	if err != nil {
		return st
	}
	screen := claude.ParseScreen(claude.LiveScreen(text, w.Lines))
	switch {
	case clear && screen.Blocker:
		return st
	case clear && screen.Working:
		return b.correct(name, in, st, sidebar.StateWorking)
	case clear && screen.PromptBox:
		return b.correct(name, in, st, sidebar.StateIdle)
	case !clear && screen.Blocker:
		return b.correct(name, in, st, sidebar.StateInput)
	}
	return st
}

// raiseDue reports whether backstop may look for a dialog on this poll: the
// row is not at input, the gate is open (no state file, a stale waiting, or
// a file whose last writer was the screen check), and raiseDebounce has
// passed since the last such read, which it then stamps.
func (b *SidebarBackend) raiseDue(in stateInput, st sidebar.State) bool {
	if st == sidebar.StateInput {
		return false
	}
	staleWaiting := claude.ParseState(in.fileState) == claude.StateWaiting &&
		!state.IsFresh(in.fileAt)
	open := in.fileState == "" || staleWaiting || in.fileSource == state.SourceScreen
	if !open {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.l.now()
	if now.Sub(b.lastRaise) < raiseDebounce {
		return false
	}
	b.lastRaise = now
	return true
}

// correct writes st to the session's state file, marked as the screen
// check's own (state.SourceScreen, which keeps the raise gate open), and
// returns it for the row, unless the file changed since the poll read it
// (in.fileAt): the text read takes a subprocess, and a hook can write in
// the meantime. Then the hook's word stands, nothing is written, and the
// row keeps prev. A failed write is dropped, as every launcher warning in
// the sidebar is.
func (b *SidebarBackend) correct(
	name string, in stateInput, prev, st sidebar.State,
) sidebar.State {
	e, err := b.readState(name)
	if err != nil {
		e.UpdatedAt = time.Time{}
	}
	if !e.UpdatedAt.Equal(in.fileAt) {
		return prev
	}
	_ = b.writeState(name, state.Entry{State: st.String(), Source: state.SourceScreen})
	return st
}

// fileOnlyState is resolveState's pi branch: the state file alone, with no
// title to arbitrate against, so its age plays no part. The visible-tab
// done→idle pass is resolveState's, applied after this.
func fileOnlyState(in stateInput) sidebar.State {
	switch claude.ParseState(in.fileState) {
	case claude.StateNeedsInput:
		return sidebar.StateInput
	case claude.StateWorking:
		return sidebar.StateWorking
	case claude.StateIdle:
		if in.fileAt.After(in.viewedAt) {
			return sidebar.StateDone
		}
		return sidebar.StateIdle
	default:
		return sidebar.StateIdle
	}
}

// title is the claude window's title minus its state glyph, or the session
// directory with $HOME shortened to ~ (sidebar.ShortenHome) when there is
// no window or no title.
func (b *SidebarBackend) title(in stateInput, dir string) string {
	if _, stripped, _ := claude.ParseTitle(in.title); stripped != "" {
		return stripped
	}
	return sidebar.ShortenHome(dir, b.home)
}

// Focus brings the session's tab to the front, recreating it when gone.
func (b *SidebarBackend) Focus(name string) error {
	_, err := b.l.Open(Request{Name: name, Resume: ResumeStored})
	return err
}

// New creates a session of the given agent kind rooted at dir; an empty
// kind means claude. An empty name takes SuggestName's. ErrExists comes back
// unchanged so the UI can ask for another name.
func (b *SidebarBackend) New(name, dir, kind string) error {
	if name == "" {
		name = SuggestName(dir)
	}
	if name == "" {
		return errors.New("launcher: a session name is required")
	}
	_, err := b.l.Open(Request{Name: name, Dir: dir, Resume: ResumeNone, Agent: kind})
	return err
}

// SuggestName is the default name for a session rooted at dir.
func (b *SidebarBackend) SuggestName(dir string) string { return SuggestName(dir) }

// TmpDir creates a scratch directory under the configured tmpdir.
func (b *SidebarBackend) TmpDir() (string, error) {
	return ScratchDir(b.cfg.EffectiveTmpDir())
}

// Close tears the session down; keep leaves the record, marked stopped.
// Closing another session signals its sidebar to close its own tab; closing
// this sidebar's own session does not, since runConfirm already withdrew its
// banner and it cannot answer a signal while it is blocked here. Tab-close
// warnings have no home in the sidebar and are dropped, as every launcher
// warning here is: the UI has one status line and the action itself
// succeeded.
func (b *SidebarBackend) Close(name string, keep bool) error {
	sess, err := b.l.store.Load(name)
	if err != nil {
		return err
	}
	_, err = b.l.Close(sess, CloseOptions{Keep: keep, Signal: sess.ID != b.ownID})
	return err
}

// Restore brings a trashed record back as a stopped session.
func (b *SidebarBackend) Restore(name string) error { return b.l.store.Restore(name) }

// Trashed lists the names in the trash.
func (b *SidebarBackend) Trashed() ([]string, error) {
	sessions, err := b.l.store.ListTrashed()
	if err != nil {
		return nil, err
	}
	names := make([]string, len(sessions))
	for i, s := range sessions {
		names[i] = s.Name
	}
	return names, nil
}

// Rename renames the record, its state file and its tab title.
func (b *SidebarBackend) Rename(oldName, newName string) error {
	_, _, err := b.l.Rename(oldName, newName)
	return err
}

// FocusAgentWindow focuses the own session's claude window and stamps
// FocusedAt. The home tab has no agent, so there it does nothing.
func (b *SidebarBackend) FocusAgentWindow() error {
	if b.ownID == "" {
		return nil
	}
	sess, lv, err := b.ownWindows()
	if err != nil {
		return err
	}
	if lv.claude == nil {
		return fmt.Errorf("%s has no claude window", sess.Name)
	}
	_, err = b.l.focus(sess, lv.claude.ID, nil)
	return err
}

// ShellSplit opens a shell below the own session's claude window, in the
// session directory, tagged with the session like every ks window.
func (b *SidebarBackend) ShellSplit() error {
	if b.ownID == "" {
		return errors.New("launcher: the home tab has no agent to split")
	}
	sess, lv, err := b.ownWindows()
	if err != nil {
		return err
	}
	if lv.claude == nil {
		return fmt.Errorf("%s has no claude window", sess.Name)
	}
	_, err = b.l.kitty.LaunchHSplit(kitty.Launch{
		Match: lv.claude.ID,
		Dir:   sess.Dir,
		Bias:  shellSplitBias,
		Vars:  []string{kitty.SessionVar + "=" + sess.ID},
	})
	if err != nil {
		return fmt.Errorf("cannot open shell split: %w", err)
	}
	return nil
}

// ownWindows loads the own session's record by id and finds its windows.
func (b *SidebarBackend) ownWindows() (*session.Session, live, error) {
	sess, err := b.l.store.FindByID(b.ownID)
	if err != nil {
		return nil, live{}, err
	}
	lv, err := b.l.liveWindows(sess)
	if err != nil {
		return nil, live{}, fmt.Errorf("cannot list kitty windows: %w", err)
	}
	return sess, lv, nil
}

// HooksStatus says how the ks hooks reach the session (injected at launch)
// and counts any leftover entries an older ks wrote to the Claude Code
// settings file, in the short form that fits the sidebar's status row
// (hooks.Status.Brief).
func (b *SidebarBackend) HooksStatus() (string, error) {
	path, err := hooks.Path()
	if err != nil {
		return "", err
	}
	st, err := hooks.Inspect(path)
	if err != nil {
		return "", err
	}
	return st.Brief(), nil
}

// Quit ends the ks instance; records stay active for the next attach. Every
// other session's sidebar is first asked to take its banner down and close
// its tab (WithdrawBanners), and this one withdraws its own banner inline,
// since it dies with its window when CloseAll runs and cannot answer a
// signal while it is blocked here. Warnings, and a store listing that fails,
// are dropped, as every launcher warning in the sidebar is.
func (b *SidebarBackend) Quit() error {
	if b.withdraw != nil {
		b.withdraw()
	}
	if sessions, err := b.l.store.List(); err == nil {
		_ = b.l.WithdrawBanners(sessions, b.ownID)
	}
	return b.quit()
}

// WithdrawAndClose takes the own session's banner down (when there is one)
// and closes the own tab (closeTabs), leaving the record alone.
// The sidebar's SIGUSR1 handler runs it when a launcher close signals this
// sidebar: only this process can take its own banner down, and it dies with
// the tab. closeTabs recreates the home tab when this was the last session
// tab. The home tab's sidebar has no own session and does nothing.
//
// The record is not read: a ks close without --keep trashes it before it
// signals, and a store lookup would then fail and leave the tab for the
// launcher to close after its whole wait. closeTabs needs only lv.tabs,
// which findLive fills from the KS_SESSION_ID tag alone, so a bare record
// carrying just the id is enough.
func (b *SidebarBackend) WithdrawAndClose() {
	if b.ownID == "" {
		return
	}
	if b.withdraw != nil {
		b.withdraw()
	}
	all, lv, err := b.l.snapshot(&session.Session{ID: b.ownID})
	if err != nil {
		return
	}
	b.l.closeTabs(all, lv)
}

// Repos lists the git repositories under the configured dirs.
func (b *SidebarBackend) Repos() ([]sidebar.Repo, error) {
	if b.cfg == nil || len(b.cfg.Dirs) == 0 {
		return nil, errors.New("no repo dirs: set dirs in ~/.config/ks/config.yaml")
	}
	repos, err := finder.Walk(b.cfg.Dirs)
	if err != nil {
		return nil, err
	}
	out := make([]sidebar.Repo, len(repos))
	for i, r := range repos {
		out[i] = sidebar.Repo{Name: r.Name, Path: r.Path}
	}
	return out, nil
}

// PinWidth resizes the own sidebar window back to the configured width when
// a terminal resize left it at another one. cols is the width the sidebar
// sees; when it already matches nothing is asked of kitty. From one instance
// snapshot it finds the own sidebar by tag and leaves it alone when it is
// gone, when it is the only window in its tab (a tab close can leave the
// sidebar alone in a layout kitty refuses to resize), or when it already has
// the configured width. The home tab never pins.
func (b *SidebarBackend) PinWidth(cols int) error {
	if b.ownID == "" || cols == b.l.sidebarWidth {
		return nil
	}
	sess, err := b.l.store.FindByID(b.ownID)
	if err != nil {
		return err
	}
	all, err := b.l.kitty.Windows()
	if err != nil {
		return fmt.Errorf("cannot list kitty windows: %w", err)
	}
	lv := findLive(all, sess)
	if lv.sidebar == nil || tabWindows(all, lv.sidebar.TabID) < 2 {
		return nil
	}
	delta := b.l.sidebarWidth - lv.sidebar.Columns
	if delta == 0 {
		return nil
	}
	return b.l.kitty.ResizeWindow(lv.sidebar.ID, kitty.AxisHorizontal, delta)
}

// Focused reports whether this sidebar's own kitty window has keyboard focus,
// from one instance snapshot. The window is the one kitty named in
// KITTY_WINDOW_ID when it launched this process; without that variable, or
// with a window the instance no longer has, the answer is false.
func (b *SidebarBackend) Focused() (bool, error) {
	id, ok := ownWindowID()
	if !ok {
		return false, nil
	}
	all, err := b.l.kitty.Windows()
	if err != nil {
		return false, fmt.Errorf("cannot list kitty windows: %w", err)
	}
	for _, w := range all {
		if w.ID == id {
			return w.Focused, nil
		}
	}
	return false, nil
}

// ownWindowID is the id of the kitty window this process runs in, from the
// KITTY_WINDOW_ID kitty sets in every window it launches. ok is false when
// the variable is unset or not a number.
func ownWindowID() (id int, ok bool) {
	id, err := strconv.Atoi(os.Getenv("KITTY_WINDOW_ID"))
	return id, err == nil
}

// tabWindows counts the windows of one tab in a snapshot.
func tabWindows(all []kitty.Window, tabID int) int {
	n := 0
	for _, w := range all {
		if w.TabID == tabID {
			n++
		}
	}
	return n
}

// tabPositions maps each tab id in the snapshot to its 1-based position in
// its OS window, in kitty's order, which is the N of goto_tab N: kitty
// numbers tabs per OS window, so a tab detached into a second one starts
// at 1 again. Every tab counts, the home tab and the user's own included.
// Stopped sessions have no tab and get no entry.
func tabPositions(all []kitty.Window) map[int]int {
	pos := map[int]int{}
	tabs := map[int]int{} // tabs seen so far, per OS window
	for _, w := range all {
		if _, seen := pos[w.TabID]; !seen {
			tabs[w.OSWindowID]++
			pos[w.TabID] = tabs[w.OSWindowID]
		}
	}
	return pos
}
