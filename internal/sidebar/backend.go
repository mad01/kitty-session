package sidebar

import "sort"

// State is an agent's state as the sidebar shows it. The constants run from
// the state that needs the user most to the one that needs them least.
type State int

const (
	// StateInput means Claude is waiting on the user: a permission prompt or a question.
	StateInput State = iota
	// StateDone means Claude finished and the user has not looked at the result yet.
	StateDone
	// StateWorking means Claude is processing.
	StateWorking
	// StateIdle means Claude is at its prompt and the result has been seen.
	StateIdle
	// StateStopped means the session has no live kitty tab.
	StateStopped
)

// String returns the lower-case label for the state.
func (s State) String() string {
	switch s {
	case StateInput:
		return "input"
	case StateDone:
		return "done"
	case StateWorking:
		return "working"
	case StateIdle:
		return "idle"
	case StateStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

// Agent is one row of the sidebar.
type Agent struct {
	Name  string
	Dir   string
	Title string // Claude's tab title, already stripped of its state glyph; empty falls back to Dir
	State State
	// Tab is the 1-based position of the agent's tab in the ks instance, the N
	// of kitty's goto_tab N (cmd+N on macOS). Zero means no tab: the session
	// is stopped or its tab is gone.
	Tab int
	// Own marks the session this sidebar belongs to. The model also treats
	// an agent whose Name equals Options.Session as own.
	Own bool
}

// Repo is one entry of the repo picker.
type Repo struct {
	Name string // org/repo as the finder names it
	Path string // absolute directory
}

// Backend performs every side effect for the sidebar. List is called on a
// 3 s tick and after each mutating action; it should cost one kitty round
// trip, not one per session.
type Backend interface {
	// List returns every agent the sidebar should show, in any order.
	List() ([]Agent, error)
	// Focus brings the named session's tab to the front, reopening it if its tab is gone.
	Focus(name string) error
	// New creates a session rooted at dir; an empty name derives one from dir.
	New(name, dir string) error
	// SuggestName returns the default session name for dir, or "" when it has none.
	SuggestName(dir string) string
	// TmpDir creates and returns a fresh scratch directory for the picker's tmp entry.
	TmpDir() (string, error)
	// Close stops the session's tab; keep leaves the record in place, otherwise it is trashed.
	Close(name string, keep bool) error
	// Restore brings a trashed session back.
	Restore(name string) error
	// Trashed lists the names Restore accepts.
	Trashed() ([]string, error)
	// Rename changes a session's name.
	Rename(oldName, newName string) error
	// FocusAgentWindow focuses the claude window of this sidebar's own tab.
	FocusAgentWindow() error
	// ShellSplit opens a shell split in this sidebar's own tab.
	ShellSplit() error
	// HooksStatus reports whether the Claude Code hooks are installed.
	HooksStatus() (string, error)
	// Quit tears ks down. The sidebar exits only after Quit returns nil.
	Quit() error
	// Repos lists the repositories the picker offers.
	Repos() ([]Repo, error)
	// PinWidth tells the host the sidebar's terminal is cols wide so it can re-pin the split.
	PinWidth(cols int) error
	// Focused reports whether this sidebar's own kitty window has keyboard focus right now.
	Focused() (bool, error)
}

// sortAgents puts agents in tab order, so the top row is cmd+1, the next
// cmd+2, and so on, whatever their states. Agents without a tab come last,
// by name, so the order is deterministic.
func sortAgents(agents []Agent) {
	sort.SliceStable(agents, func(i, j int) bool {
		a, b := agents[i], agents[j]
		if (a.Tab == 0) != (b.Tab == 0) {
			return a.Tab != 0
		}
		if a.Tab != b.Tab {
			return a.Tab < b.Tab
		}
		return a.Name < b.Name
	})
}
