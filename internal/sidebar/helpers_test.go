package sidebar

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeBackend records every call so tests can assert on dispatch.
type fakeBackend struct {
	agents    []Agent
	trashed   []string
	repos     []Repo
	calls     []string
	pinned    int
	pinErr    error
	fail      error  // returned by every mutating call when set
	noSuggest bool   // SuggestName returns ""
	tmpBase   string // when set, TmpDir creates a real directory under it
}

func (f *fakeBackend) record(format string, args ...any) error {
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
	return f.fail
}

func (f *fakeBackend) List() ([]Agent, error)       { return f.agents, nil }
func (f *fakeBackend) Focus(name string) error      { return f.record("focus:%s", name) }
func (f *fakeBackend) New(name, dir string) error   { return f.record("new:%s:%s", name, dir) }
func (f *fakeBackend) Restore(name string) error    { return f.record("restore:%s", name) }
func (f *fakeBackend) Trashed() ([]string, error)   { return f.trashed, nil }
func (f *fakeBackend) FocusAgentWindow() error      { return f.record("focus-agent") }
func (f *fakeBackend) ShellSplit() error            { return f.record("shell-split") }
func (f *fakeBackend) HooksStatus() (string, error) { return "hooks ok", f.record("hooks") }
func (f *fakeBackend) Quit() error                  { return f.record("quit") }
func (f *fakeBackend) Repos() ([]Repo, error)       { return f.repos, nil }

func (f *fakeBackend) SuggestName(dir string) string {
	if f.noSuggest {
		return ""
	}
	return "sug-" + dir[strings.LastIndex(dir, "/")+1:]
}

func (f *fakeBackend) TmpDir() (string, error) {
	f.calls = append(f.calls, "tmpdir")
	if f.tmpBase == "" {
		return "/tmp/ks-fake", nil
	}
	return os.MkdirTemp(f.tmpBase, "ks-")
}

func (f *fakeBackend) Close(name string, keep bool) error {
	return f.record("close:%s:%v", name, keep)
}

func (f *fakeBackend) Rename(oldName, newName string) error {
	return f.record("rename:%s:%s", oldName, newName)
}

func (f *fakeBackend) PinWidth(cols int) error {
	f.pinned = cols
	return f.pinErr
}

var errFake = errors.New("fake failure")

// testNow is the fixed clock the test models run on.
var testNow = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

// mockupAgents mirrors the approved mockup plus a working and a stopped row.
// kitty-session is the own session, as the backend would mark it.
func mockupAgents() []Agent {
	return []Agent{
		{
			Name: "kitty-session", Dir: "/home/u/code/kitty-session", Title: "Claude Code",
			State: StateIdle, ChangedAt: testNow.Add(-25 * time.Minute), Own: true,
		},
		{
			Name: "migraine-me", Dir: "/home/u/code/migraine-me", Title: "Migrane iOS 27.1 update",
			State: StateIdle, ChangedAt: testNow.Add(-20 * time.Minute),
		},
		{
			Name: "dropbrain-app", Dir: "/home/u/code/dropbrain-app",
			Title: "Migrane iOS 27.1 migration ✳ follow-ups",
			State: StateDone, ChangedAt: testNow.Add(-3 * time.Minute),
		},
		{
			Name: "thismoon", Dir: "/home/u/code/thismoon",
			Title: "Mods overview and integration plan",
			State: StateInput, ChangedAt: testNow.Add(-time.Minute),
		},
		{
			Name: "code-search-local", Dir: "/home/u/code/code-search-local",
			Title: "Reindex 日本語 テスト after sparse checkout",
			State: StateWorking, ChangedAt: testNow.Add(-10 * time.Second),
		},
		{
			Name: "dotfiles", Dir: "/home/u/code/dotfiles", State: StateStopped,
			ChangedAt: testNow.Add(-2 * time.Hour),
		},
	}
}

// testSize is the terminal the tests pretend to run in.
const (
	testCols  = 80
	testLines = 24
)

// newTestModel returns a sized model with the fake's agents loaded.
func newTestModel(t *testing.T, fb *fakeBackend, session string) model {
	t.Helper()
	return newSizedModel(t, fb, session, DefaultWidth, testLines)
}

// newSizedModel is newTestModel with a frame width and terminal height.
func newSizedModel(t *testing.T, fb *fakeBackend, session string, width, height int) model {
	t.Helper()
	m := newModel(Options{Session: session, Width: width, Backend: fb}, "/home/u")
	m.now = func() time.Time { return testNow }
	m = update(t, m, tea.WindowSizeMsg{Width: testCols, Height: height})
	return load(t, m)
}

// load runs a List and applies its result.
func load(t *testing.T, m model) model {
	t.Helper()
	cmd := m.listCmd()
	return update(t, m, cmd())
}

// update feeds one message and returns the concrete model.
func update(t *testing.T, m model, msg tea.Msg) model {
	t.Helper()
	next, _ := m.Update(msg)
	return asModel(t, next)
}

// press feeds keys in order, returning the model and the last command.
func press(t *testing.T, m model, keys ...string) (model, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, k := range keys {
		var next tea.Model
		next, cmd = m.Update(key(k))
		m = asModel(t, next)
	}
	return m, cmd
}

// run presses keys and then runs the resulting command, applying what it
// produces: the way an asynchronous backend action lands in the model.
// Do not use it on commands that include a text input focus, whose blink
// timer would stall the test.
func run(t *testing.T, m model, keys ...string) (model, tea.Cmd) {
	t.Helper()
	m, cmd := press(t, m, keys...)
	return feed(t, m, cmd)
}

// feed runs cmd and applies every message it produces, returning the last
// command the model issued in response.
func feed(t *testing.T, m model, cmd tea.Cmd) (model, tea.Cmd) {
	t.Helper()
	var last tea.Cmd
	for _, msg := range drain(cmd) {
		var next tea.Model
		next, last = m.Update(msg)
		m = asModel(t, next)
	}
	return m, last
}

func asModel(t *testing.T, tm tea.Model) model {
	t.Helper()
	m, ok := tm.(model)
	if !ok {
		t.Fatalf("Update returned %T, want model", tm)
	}
	return m
}

// key builds the KeyMsg for a key name or a run of typed runes.
func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// click is a left mouse press at terminal column x and row y.
func click(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
}

// drain executes cmd, flattening batches, and returns every message produced.
func drain(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, drain(c)...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

func wantCalls(t *testing.T, fb *fakeBackend, want ...string) {
	t.Helper()
	got := strings.Join(fb.calls, ",")
	if got != strings.Join(want, ",") {
		t.Fatalf("calls = %q, want %q", got, strings.Join(want, ","))
	}
}

func names(agents []Agent) []string {
	out := make([]string, len(agents))
	for i, a := range agents {
		out[i] = a.Name
	}
	return out
}
