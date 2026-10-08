package sidebar

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// DemoBackend is an in-memory Backend with eight fake agents covering every
// state and agent kind and a fake repo list, so the sidebar can be reviewed
// in any terminal without kitty. Mutations are kept in memory; nothing touches the system.
type DemoBackend struct {
	mu      sync.Mutex
	agents  []Agent
	trashed []string
	repos   []Repo
	pinned  int
}

// NewDemoBackend returns a DemoBackend with the mockup's agents.
func NewDemoBackend() *DemoBackend {
	home, _ := os.UserHomeDir()
	src := filepath.Join(home, "code", "src", "github.com", "mad01")
	return &DemoBackend{
		agents: []Agent{
			{
				Name: "thismoon", Dir: filepath.Join(src, "thismoon"),
				Title: "Mods overview and integration plan for the events service",
				State: StateInput, Tab: 1, Waiting: 12 * time.Minute,
			},
			{
				Name: "dropbrain-app", Dir: filepath.Join(src, "dropbrain-app"),
				Title: "Migrane iOS 27.1 migration ✳ follow-ups",
				State: StateDone, Tab: 2,
			},
			{
				Name: "code-search-local", Dir: filepath.Join(src, "code-search-local"),
				Title: "Reindex after sparse checkout 日本語 テスト",
				State: StateWorking, Tab: 3,
			},
			{
				Name: "migraine-me", Dir: filepath.Join(src, "migraine-me"),
				Title: "Migrane iOS 27.1 update",
				State: StateIdle, Tab: 4,
			},
			{
				Name: "kitty-session", Dir: filepath.Join(src, "kitty-session"),
				Title: "Claude Code",
				State: StateIdle, Tab: 5,
			},
			{
				Name: "events-pi", Kind: "pi", Dir: filepath.Join(src, "thismoon"),
				Title: "pi", State: StateWorking, Tab: 6,
			},
			{
				Name: "dotfiles-sh", Kind: "shell", Dir: filepath.Join(src, "dotfiles"),
				State: StateIdle, Tab: 7,
			},
			{
				Name: "dotfiles", Dir: filepath.Join(src, "dotfiles"),
				State: StateStopped,
			},
		},
		trashed: []string{"old-experiment", "spike-2026-09"},
		repos: []Repo{
			{Name: "mad01/thismoon", Path: filepath.Join(src, "thismoon")},
			{Name: "mad01/kitty-session", Path: filepath.Join(src, "kitty-session")},
			{Name: "mad01/code-search-local", Path: filepath.Join(src, "code-search-local")},
			{Name: "mad01/dotfiles", Path: filepath.Join(src, "dotfiles")},
			{Name: "mad01/brain", Path: filepath.Join(src, "brain")},
		},
	}
}

// SetOwn marks the named agent as this sidebar's own session and clears
// the flag on every other agent.
func (d *DemoBackend) SetOwn(name string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i := range d.agents {
		d.agents[i].Own = d.agents[i].Name == name
	}
}

// List returns a copy of the agents.
func (d *DemoBackend) List() ([]Agent, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]Agent, len(d.agents))
	copy(out, d.agents)
	return out, nil
}

// Focus marks a done agent as seen, so it drops to idle like the real thing.
func (d *DemoBackend) Focus(name string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	i := d.index(name)
	if i < 0 {
		return fmt.Errorf("demo: no agent %q", name)
	}
	if d.agents[i].State == StateDone {
		d.agents[i].State = StateIdle
	}
	return nil
}

// New adds an idle agent of the given kind; a name that is taken is
// rejected like the launcher does.
func (d *DemoBackend) New(name, dir, kind string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if name == "" {
		name = filepath.Base(dir)
	}
	if d.index(name) >= 0 {
		return fmt.Errorf("demo: session %q already exists", name)
	}
	title := ""
	if kindBadge(kind) == "" {
		title = "Claude Code"
	}
	d.agents = append(d.agents, Agent{
		Name: name, Kind: kind, Dir: dir, Title: title, State: StateIdle, Tab: d.nextTab(),
	})
	return nil
}

// SuggestName derives a name from the directory's base name.
func (d *DemoBackend) SuggestName(dir string) string {
	return filepath.Base(dir)
}

// TmpDir returns a fake scratch path without creating it.
func (d *DemoBackend) TmpDir() (string, error) {
	return filepath.Join(os.TempDir(), fmt.Sprintf("ks-demo-%d", time.Now().Unix())), nil
}

// Close stops the agent; without keep it moves to the trash list.
func (d *DemoBackend) Close(name string, keep bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	i := d.index(name)
	if i < 0 {
		return fmt.Errorf("demo: no agent %q", name)
	}
	if keep {
		d.agents[i].State = StateStopped
		d.agents[i].Tab = 0
		return nil
	}
	d.agents = append(d.agents[:i], d.agents[i+1:]...)
	d.trashed = append(d.trashed, name)
	return nil
}

// Restore moves a name from the trash list back as a stopped agent.
func (d *DemoBackend) Restore(name string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, t := range d.trashed {
		if t == name {
			d.trashed = append(d.trashed[:i], d.trashed[i+1:]...)
			d.agents = append(d.agents, Agent{
				Name: name, Title: "restored", State: StateStopped,
			})
			return nil
		}
	}
	return fmt.Errorf("demo: %q is not in the trash", name)
}

// Trashed returns the trash list, sorted.
func (d *DemoBackend) Trashed() ([]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]string, len(d.trashed))
	copy(out, d.trashed)
	sort.Strings(out)
	return out, nil
}

// Rename changes an agent's name.
func (d *DemoBackend) Rename(oldName, newName string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.index(newName) >= 0 {
		return fmt.Errorf("demo: session %q already exists", newName)
	}
	i := d.index(oldName)
	if i < 0 {
		return fmt.Errorf("demo: no agent %q", oldName)
	}
	d.agents[i].Name = newName
	return nil
}

// FocusAgentWindow does nothing: there is no kitty window in the demo.
func (d *DemoBackend) FocusAgentWindow() error { return nil }

// ShellSplit does nothing in the demo.
func (d *DemoBackend) ShellSplit() error { return nil }

// HooksStatus reports a canned answer.
func (d *DemoBackend) HooksStatus() (string, error) {
	return "hooks installed (demo)", nil
}

// Quit succeeds so the demo program exits.
func (d *DemoBackend) Quit() error { return nil }

// Repos returns the fake repo list.
func (d *DemoBackend) Repos() ([]Repo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]Repo, len(d.repos))
	copy(out, d.repos)
	return out, nil
}

// PinWidth records the width; the demo has no split to re-pin.
func (d *DemoBackend) PinWidth(cols int) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pinned = cols
	return nil
}

// Focused reports no focus: the demo has no kitty window to ask, and the
// terminal's own focus events take over from the first change.
func (d *DemoBackend) Focused() (bool, error) { return false, nil }

// index returns the position of the named agent, or -1. Callers hold mu.
func (d *DemoBackend) index(name string) int {
	for i, a := range d.agents {
		if a.Name == name {
			return i
		}
	}
	return -1
}

// nextTab is the position a new tab takes: after the last one.
func (d *DemoBackend) nextTab() int {
	n := 0
	for _, a := range d.agents {
		n = max(n, a.Tab)
	}
	return n + 1
}
