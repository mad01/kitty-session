package launcher

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
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
	// text that look for a dialog (backstop's raise), which can go on for a
	// whole session when the hooks are not installed.
	raiseDebounce = 10 * time.Second
	// hooksRecheck is how long backstop trusts its last look at whether the
	// ks hooks are installed.
	hooksRecheck = time.Minute
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
	readState func(name string) (string, time.Time, error)
	// writeState writes a session's state file, for backstop; tests
	// substitute a fake.
	writeState func(name, st string) error
	// readText reads the live bottom of a window's terminal text, for
	// backstop; tests substitute a fake.
	readText func(windowID int) (string, error)
	// hooksInstalled reports whether the ks hooks are registered with
	// Claude Code, for backstop's raise gate; tests substitute a fake.
	hooksInstalled func() bool

	mu         sync.Mutex // guards the fields below; List runs off the UI loop
	lastViewed time.Time
	lastRaise  time.Time // when backstop last read the text for a dialog
	hooksAt    time.Time // when hooksOK was last read, zero before the first
	hooksOK    bool
	reap       reaper // closes the own tab once its claude is gone; see reapIfGone
}

// NewSidebarBackend returns the backend for the instance behind client.
// cfg may be nil; ownID is the id of the session whose tab this sidebar sits
// in (empty in the home tab).
func NewSidebarBackend(
	store *session.Store,
	client *kitty.Client,
	cfg *config.Config,
	ownID string,
) (*SidebarBackend, error) {
	l, err := New(store, client, cfg)
	if err != nil {
		return nil, err
	}
	b := newSidebarBackend(l, cfg, ownID)
	b.quit = func() error { return instance.Shutdown(client) }
	b.readText = client.GetTextAll
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
		readState:  state.Read,
		writeState: state.Write,
		readText: func(int) (string, error) {
			return "", errors.New("launcher: text reader is not wired")
		},
		hooksInstalled: ksHooksInstalled,
	}
}

// ksHooksInstalled reports whether any ks hook event is registered in
// Claude Code's settings. Settings that cannot be read count as none: Claude
// Code would not run the hooks from them either.
func ksHooksInstalled() bool {
	path, err := hooks.Path()
	if err != nil {
		return false
	}
	installed, err := hooks.Installed(path)
	return err == nil && len(installed) > 0
}

// List returns one agent per session record from one store listing and one
// instance snapshot. Windows are matched to records by their session tag.
// When the own session's tab is the active one, its record is stamped as
// viewed (at most once per viewedDebounce) before its state is resolved.
// Once the own session's claude window is gone, its tab is closed from the
// same snapshot (reapIfGone).
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
		if own && lv.tabActive() {
			if sess, err = b.markViewed(sess); err != nil {
				return nil, err
			}
		}
		agents = append(agents, b.agent(sess, lv, own, pos[lv.tabID()]))
		if own {
			b.reapIfGone(all, lv)
		}
	}
	return agents, nil
}

// markViewed stamps ViewedAt on a fresh copy of the record, so a hook update
// made in the meantime is kept, unless the last stamp is younger than
// viewedDebounce. It returns the record to resolve state from.
func (b *SidebarBackend) markViewed(sess *session.Session) (*session.Session, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.l.now()
	if now.Sub(b.lastViewed) < viewedDebounce {
		return sess, nil
	}
	fresh, err := b.l.store.Load(sess.Name)
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
		tabActive: lv.tabActive(),
	}
	if w != nil {
		in.hasWindow, in.title, in.report = true, w.Title, w.Status
	}
	if s, at, err := b.readState(sess.Name); err == nil {
		in.fileState, in.fileAt = s, at
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
	}
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
	viewedAt  time.Time
	tabActive bool // the session's tab is the one its OS window shows
	// report is the state of the agent window's Program Status record, ""
	// without one.
	report kitty.ProgramState
}

// resolveState picks the sidebar state for one session. Whatever the kind,
// a stopped record or a missing agent window is stopped. Two rules then
// apply whatever the kind, in this order:
//
//	state file fresh (state.IsFresh)                → the kind's own rules below: the hooks saw the event
//	agent window has a Program Status record        → blocked is input; working, done, error and idle are themselves, except done on the active tab, which is idle
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
// all, or are not installed, and no record speaks, it reads the window's
// text and corrects the state from what is on screen, writing the
// correction to the state file. It is the single writer of that
// correction, so every other sidebar, which never reads the text, and ks
// list follow through the file.
func resolveState(in stateInput) sidebar.State {
	if !in.active || !in.hasWindow {
		return sidebar.StateStopped
	}
	if st, ok := ReportedState(in.report, in.fileAt, in.tabActive); ok {
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
// startup, where the folder-trust dialog comes before any hook fires, or
// the ks hooks not installed, where nothing else would ever report a
// prompt. A clear reads the text on every poll, since that wait is the
// user's; a raise reads at most once per raiseDebounce. The text is cut to
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
// the ks hooks not installed), and raiseDebounce has passed since the last
// such read, which it then stamps.
func (b *SidebarBackend) raiseDue(in stateInput, st sidebar.State) bool {
	if st == sidebar.StateInput {
		return false
	}
	staleWaiting := claude.ParseState(in.fileState) == claude.StateWaiting &&
		!state.IsFresh(in.fileAt)
	open := in.fileState == "" || staleWaiting || !b.hooksOn()
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

// hooksOn reports whether the ks hooks are installed, re-reading the
// settings at most once per hooksRecheck.
func (b *SidebarBackend) hooksOn() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.l.now()
	if b.hooksAt.IsZero() || now.Sub(b.hooksAt) >= hooksRecheck {
		b.hooksOK, b.hooksAt = b.hooksInstalled(), now
	}
	return b.hooksOK
}

// correct writes st to the session's state file and returns it for the row,
// unless the file changed since the poll read it (in.fileAt): the text read
// takes a subprocess, and a hook can write in the meantime. Then the hook's
// word stands, nothing is written, and the row keeps prev. A failed write
// is dropped, as every launcher warning in the sidebar is.
func (b *SidebarBackend) correct(
	name string, in stateInput, prev, st sidebar.State,
) sidebar.State {
	_, at, err := b.readState(name)
	if err != nil {
		at = time.Time{}
	}
	if !at.Equal(in.fileAt) {
		return prev
	}
	_ = b.writeState(name, st.String())
	return st
}

// fileOnlyState is resolveState's pi branch: the state file alone, with no
// title to arbitrate against, so its age plays no part.
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
// directory with $HOME shortened to ~ when there is no window or no title.
func (b *SidebarBackend) title(in stateInput, dir string) string {
	if _, stripped, _ := claude.ParseTitle(in.title); stripped != "" {
		return stripped
	}
	if b.home != "" && strings.HasPrefix(dir, b.home) {
		return "~" + dir[len(b.home):]
	}
	return dir
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
// Tab-close warnings have no home in the sidebar and are dropped, as every
// launcher warning here is: the UI has one status line and the action itself
// succeeded.
func (b *SidebarBackend) Close(name string, keep bool) error {
	sess, err := b.l.store.Load(name)
	if err != nil {
		return err
	}
	_, err = b.l.Close(sess, keep)
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

// HooksStatus says which of the ks hook events are registered with Claude Code.
func (b *SidebarBackend) HooksStatus() (string, error) {
	path, err := hooks.Path()
	if err != nil {
		return "", err
	}
	installed, err := hooks.Installed(path)
	if err != nil {
		return "", err
	}
	return hooksSummary(installed), nil
}

// hooksSummary fits the hook status on one sidebar line: all, none, or the
// events that are missing.
func hooksSummary(installed []string) string {
	switch len(installed) {
	case 0:
		return "hooks: none registered (ks hooks install)"
	case len(hooks.Events):
		return fmt.Sprintf("hooks: all %d events registered", len(hooks.Events))
	}
	var missing []string
	for _, e := range hooks.Events {
		if !slices.Contains(installed, e) {
			missing = append(missing, e)
		}
	}
	return "hooks: missing " + strings.Join(missing, ", ")
}

// Quit ends the ks instance; records stay active for the next attach.
func (b *SidebarBackend) Quit() error { return b.quit() }

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
// kitty's order, which is the N of goto_tab N (cmd+N). Stopped sessions have
// no tab and get no entry.
func tabPositions(all []kitty.Window) map[int]int {
	pos := map[int]int{}
	for _, w := range all {
		if _, seen := pos[w.TabID]; !seen {
			pos[w.TabID] = len(pos) + 1
		}
	}
	return pos
}
