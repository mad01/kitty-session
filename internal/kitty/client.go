// Package kitty drives the ks-owned kitty instance over kitty's remote-control
// protocol. Client is the only place in ks that runs `kitty @`, and
// Client.Start the only place that runs the kitty binary itself. The package
// knows nothing about sessions beyond SessionVar, the user variable that tags
// every window ks launches.
package kitty

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"
)

// pingTimeout bounds a liveness probe of the instance socket.
const pingTimeout = 2 * time.Second

// SessionVar is the kitty user variable every ks-launched window carries, set
// to the owning session's ID. Kitty reuses window ids across instances, so
// this tag, not the id, says which session a window belongs to.
const SessionVar = "KS_SESSION_ID"

// Layout names ks sets on its tabs.
const (
	// LayoutSplits is the kitty layout that honours --location=vsplit.
	LayoutSplits = "splits"
)

// Axis names for ResizeWindow.
const (
	AxisHorizontal = "horizontal"
	AxisVertical   = "vertical"
)

// ErrNotFound is returned when a window or tab id is not in the instance.
var ErrNotFound = errors.New("kitty: not found")

// instanceOverrides are the settings the ks topology depends on: remote
// control, no tab bar (the sidebar is the tab list), no borders or margins
// between the sidebar and claude, a little padding, and quitting with the last
// window so CloseAll ends the instance.
var instanceOverrides = []string{
	"allow_remote_control=yes",
	"tab_bar_style=hidden",
	"window_border_width=0",
	"window_margin_width=0",
	"window_padding_width=3",
	"macos_quit_when_last_window_closed=yes",
}

// instanceMaps are the ks keyboard chords, passed to kitty as -o like the
// overrides: ctrl+b then s lands on the sidebar (left of claude), ctrl+b then
// a lands back on the agent. They come after instanceOverrides and before the
// user's kitty_overrides, so a user map wins.
var instanceMaps = []string{
	"map ctrl+b>s neighboring_window left",
	"map ctrl+b>a neighboring_window right",
}

// Environment hygiene. The instance inherits the environment of the process
// that starts it and hands it to every window it hosts. Started from inside a
// Claude Code session, that would make every claude in it a child session
// with transcript saving off; started from the user's kitty, its children
// would believe they live in that kitty.
var (
	// scrubPrefixes name the variables Start drops from the instance's
	// environment.
	scrubPrefixes = []string{"CLAUDE", "KS_", "KITTY_"}
	// keepNames are exempt from scrubPrefixes: the instance must read the
	// same kitty.conf as the user's kitty.
	keepNames = []string{"KITTY_CONFIG_DIRECTORY"}
	// unsetInWindows are removed from every window ks launches (kitty's
	// `--env NAME` with no value unsets), covering an instance that was
	// started with them by an older ks or by hand.
	unsetInWindows = []string{
		"CLAUDECODE",
		"CLAUDE_CODE_CHILD_SESSION",
		"CLAUDE_CODE_SESSION_ID",
		"CLAUDE_PID",
		"CLAUDE_CODE_ENTRYPOINT",
	}
)

// scrubEnv returns environ without the variables ks must not leak into the
// instance: every name with one of scrubPrefixes except keepNames.
func scrubEnv(environ []string) []string {
	out := make([]string, 0, len(environ))
	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		if slices.Contains(keepNames, name) || !hasScrubPrefix(name) {
			out = append(out, kv)
		}
	}
	return out
}

func hasScrubPrefix(name string) bool {
	return slices.ContainsFunc(scrubPrefixes, func(p string) bool {
		return strings.HasPrefix(name, p)
	})
}

// runner executes `kitty @` with args and returns its stdout.
type runner func(ctx context.Context, args ...string) ([]byte, error)

// spawner starts the kitty binary itself with the given environment.
type spawner func(env []string, args ...string) error

// Client talks to one kitty instance through its remote-control socket.
type Client struct {
	socket string
	run    runner
	spawn  spawner
}

// New returns a client for the instance listening on socket (unix:<path>).
// Nothing is contacted until a method runs.
func New(socket string) *Client {
	return &Client{socket: socket, run: runKitty, spawn: spawnKitty}
}

// Socket returns the address the client talks to.
func (c *Client) Socket() string { return c.socket }

func runKitty(ctx context.Context, args ...string) ([]byte, error) {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "kitty", args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, withStderr(err, stderr.Bytes())
	}
	return out, nil
}

func spawnKitty(env []string, args ...string) error {
	cmd := exec.Command("kitty", args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return withStderr(err, out)
	}
	return nil
}

// withStderr attaches kitty's own message to an exit error when there is one.
func withStderr(err error, out []byte) error {
	if msg := strings.TrimSpace(string(out)); msg != "" {
		return fmt.Errorf("%w: %s", err, msg)
	}
	return err
}

// at runs one `kitty @` subcommand against the instance socket.
func (c *Client) at(args ...string) ([]byte, error) {
	return c.atContext(context.Background(), args...)
}

func (c *Client) atContext(ctx context.Context, args ...string) ([]byte, error) {
	full := append([]string{"@", "--to", c.socket}, args...)
	out, err := c.run(ctx, full...)
	if err != nil {
		return nil, fmt.Errorf("kitty @ %s: %w", args[0], err)
	}
	return out, nil
}

// StartOptions describes the instance Start creates.
type StartOptions struct {
	// Overrides are extra key=value kitty settings, applied after the ones
	// ks requires so they can tune but not disable the topology.
	Overrides []string
	// Command runs in the first window. The instance exits when its last
	// window closes.
	Command []string
	// Title is the OS window title.
	Title string
}

// Start launches a detached kitty instance listening on the client's socket,
// with the caller's environment minus the variables in scrubPrefixes. It
// returns once kitty has forked; Ping says when the socket answers.
func (c *Client) Start(opts StartOptions) error {
	args := []string{"--detach", "--listen-on", c.socket}
	for _, o := range slices.Concat(instanceOverrides, instanceMaps, opts.Overrides) {
		args = append(args, "-o", o)
	}
	if opts.Title != "" {
		args = append(args, "--title", opts.Title)
	}
	args = append(args, "--")
	args = append(args, opts.Command...)
	if err := c.spawn(scrubEnv(os.Environ()), args...); err != nil {
		return fmt.Errorf("kitty --detach: %w", err)
	}
	return nil
}

// Ping reports whether the instance answers on its socket within pingTimeout.
func (c *Client) Ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()
	_, err := c.atContext(ctx, "ls")
	return err
}

// Window is one kitty window (pane) as reported by `kitty @ ls`.
type Window struct {
	ID       int
	TabID    int
	TabTitle string
	// TabActive is true when the window's tab is the one its OS window
	// shows, whether or not that OS window has keyboard focus.
	TabActive bool
	// Title is the window title. Claude sets it through OSC while it runs,
	// which is why ks never passes --title to the claude window.
	Title   string
	Columns int
	// SessionID is the SessionVar user variable: the owning ks session's ID,
	// empty for windows ks did not launch.
	SessionID string
}

// ls JSON shapes, limited to the fields ks reads.
type (
	lsOSWindow struct {
		Tabs []lsTab `json:"tabs"`
	}
	lsTab struct {
		ID       int        `json:"id"`
		Title    string     `json:"title"`
		IsActive bool       `json:"is_active"`
		Windows  []lsWindow `json:"windows"`
	}
	lsWindow struct {
		ID       int               `json:"id"`
		Title    string            `json:"title"`
		Columns  int               `json:"columns"`
		UserVars map[string]string `json:"user_vars"`
	}
)

// Windows returns every window in the instance, in kitty's order (OS window,
// tab, window). One call answers every existence and geometry question, so
// callers that ask several take one snapshot.
func (c *Client) Windows() ([]Window, error) {
	out, err := c.at("ls")
	if err != nil {
		return nil, err
	}
	return parseWindows(out)
}

func parseWindows(data []byte) ([]Window, error) {
	var osWindows []lsOSWindow
	if err := json.Unmarshal(data, &osWindows); err != nil {
		return nil, fmt.Errorf("cannot parse kitty @ ls output: %w", err)
	}
	var windows []Window
	for _, osw := range osWindows {
		for _, t := range osw.Tabs {
			for _, w := range t.Windows {
				windows = append(windows, Window{
					ID:        w.ID,
					TabID:     t.ID,
					TabTitle:  t.Title,
					TabActive: t.IsActive,
					Title:     w.Title,
					Columns:   w.Columns,
					SessionID: w.UserVars[SessionVar],
				})
			}
		}
	}
	return windows, nil
}

// window returns the window with id from a fresh snapshot.
func (c *Client) window(id int) (Window, error) {
	windows, err := c.Windows()
	if err != nil {
		return Window{}, err
	}
	i := slices.IndexFunc(windows, func(w Window) bool { return w.ID == id })
	if i < 0 {
		return Window{}, fmt.Errorf("window %d: %w", id, ErrNotFound)
	}
	return windows[i], nil
}

// AnyWindow returns the id of the first window in the instance, the anchor
// for creating tabs in its OS window.
func (c *Client) AnyWindow() (int, error) {
	windows, err := c.Windows()
	if err != nil {
		return 0, err
	}
	if len(windows) == 0 {
		return 0, fmt.Errorf("instance has no windows: %w", ErrNotFound)
	}
	return windows[0].ID, nil
}

// TabExists reports whether a tab with the given id is in the instance. An
// unreachable instance counts as no.
func (c *Client) TabExists(tabID int) bool {
	windows, err := c.Windows()
	if err != nil {
		return false
	}
	return slices.ContainsFunc(windows, func(w Window) bool { return w.TabID == tabID })
}

// WindowExists reports whether a window with the given id is in the instance.
// An unreachable instance counts as no.
func (c *Client) WindowExists(windowID int) bool {
	_, err := c.window(windowID)
	return err == nil
}

// FindTabForWindow returns the id of the tab containing the window.
func (c *Client) FindTabForWindow(windowID int) (int, error) {
	w, err := c.window(windowID)
	if err != nil {
		return 0, err
	}
	return w.TabID, nil
}

// WindowColumns returns the window's width in cells.
func (c *Client) WindowColumns(windowID int) (int, error) {
	w, err := c.window(windowID)
	if err != nil {
		return 0, err
	}
	return w.Columns, nil
}

// WindowTitle returns the window's current title.
func (c *Client) WindowTitle(windowID int) (string, error) {
	w, err := c.window(windowID)
	if err != nil {
		return "", err
	}
	return w.Title, nil
}

// Launch describes a window to create.
type Launch struct {
	// Match is the id of an existing window: any window in the target OS
	// window for LaunchTab, the window to split for LaunchVSplit and
	// LaunchHSplit.
	Match int
	// Dir is the new window's working directory.
	Dir string
	// Env are KEY=VALUE pairs exported into the new window's process, on top
	// of the instance's environment minus unsetInWindows.
	Env []string
	// Vars are KEY=VALUE kitty user variables set on the new window.
	Vars []string
	// Bias is the share of the split the new window takes, in percent.
	// LaunchVSplit and LaunchHSplit only.
	Bias int
	// Command runs in the new window.
	Command []string
}

// LaunchTab creates a tab in the OS window containing l.Match and returns the
// id of the tab's first window.
func (c *Client) LaunchTab(l Launch) (int, error) {
	return c.launch([]string{"launch", "--type=tab", matchID(l.Match)}, l)
}

// LaunchVSplit splits window l.Match side by side and returns the new
// window's id. The tab must be in the splits layout (GotoLayout) or kitty
// ignores the location. The split is relative to the tab's active window,
// which at the one call site is l.Match itself.
func (c *Client) LaunchVSplit(l Launch) (int, error) {
	args := []string{
		"launch", "--type=window", "--location=vsplit",
		"--bias=" + strconv.Itoa(l.Bias), matchID(l.Match),
	}
	return c.launch(args, l)
}

// LaunchHSplit creates a window below l.Match and returns its id. Unlike
// LaunchVSplit it names the window to split with --next-to, so it works while
// another window of the tab (the sidebar) is active; --match still selects
// the tab, without which kitty ignores --next-to.
func (c *Client) LaunchHSplit(l Launch) (int, error) {
	args := []string{
		"launch", "--type=window", "--location=hsplit",
		"--bias=" + strconv.Itoa(l.Bias), matchID(l.Match),
		"--next-to=id:" + strconv.Itoa(l.Match),
	}
	return c.launch(args, l)
}

func (c *Client) launch(args []string, l Launch) (int, error) {
	if l.Dir != "" {
		args = append(args, "--cwd="+l.Dir)
	}
	for _, name := range unsetInWindows {
		args = append(args, "--env", name)
	}
	for _, e := range l.Env {
		args = append(args, "--env", e)
	}
	for _, v := range l.Vars {
		args = append(args, "--var", v)
	}
	args = append(args, "--")
	args = append(args, l.Command...)
	out, err := c.at(args...)
	if err != nil {
		return 0, err
	}
	id, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, fmt.Errorf("cannot parse window id: %w (output: %q)", err, string(out))
	}
	return id, nil
}

// GotoLayout switches the tab containing the window to the named layout.
func (c *Client) GotoLayout(windowID int, layout string) error {
	_, err := c.at("goto-layout", matchID(windowID), layout)
	return err
}

// LayoutAction runs a layout_action (for example move_to_screen_edge left)
// in the tab containing the window. Kitty applies layout actions to the
// tab's active window, so focus the window to act on first.
func (c *Client) LayoutAction(windowID int, args ...string) error {
	full := append([]string{"action", matchID(windowID), "layout_action"}, args...)
	_, err := c.at(full...)
	return err
}

// ResizeWindow grows (positive) or shrinks (negative) the window by increment
// cells along the axis.
func (c *Client) ResizeWindow(windowID int, axis string, increment int) error {
	_, err := c.at(
		"resize-window", matchID(windowID),
		"--axis="+axis, "--increment="+strconv.Itoa(increment),
	)
	return err
}

// FocusWindow focuses the window and the tab containing it.
func (c *Client) FocusWindow(windowID int) error {
	_, err := c.at("focus-window", matchID(windowID))
	return err
}

// SetTabTitleForWindow sets the title of the tab containing the window
// without changing focus.
func (c *Client) SetTabTitleForWindow(title string, windowID int) error {
	_, err := c.at("set-tab-title", matchID(windowID), title)
	return err
}

// CloseTab closes a tab by its id.
func (c *Client) CloseTab(tabID int) error {
	_, err := c.at("close-tab", "--match=id:"+strconv.Itoa(tabID))
	return err
}

// CloseWindow closes a window by its id.
func (c *Client) CloseWindow(windowID int) error {
	_, err := c.at("close-window", matchID(windowID))
	return err
}

// CloseAll closes every window in the instance, which ends it.
func (c *Client) CloseAll() error {
	_, err := c.at("close-window", "--match=all")
	return err
}

// GetText reads the terminal text of the window.
func (c *Client) GetText(windowID int) (string, error) {
	out, err := c.at("get-text", matchID(windowID))
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func matchID(windowID int) string {
	return "--match=id:" + strconv.Itoa(windowID)
}
