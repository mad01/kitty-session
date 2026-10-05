// Package sidebar renders the agent list ks shows beside every Claude Code
// session: a fixed-width, full-height Bubble Tea view listing each agent with
// its state, letting the user jump between agents and run the session actions
// (new, rename, close, delete, restore) from keys, mouse clicks or a small menu.
//
// Every side effect goes through the Backend interface, so the same view runs
// against kitty in production and against the in-memory demo backend from
// NewDemoBackend. The view never quits on its own; the only exit is the
// menu's "quit ks" entry, which asks the backend to tear ks down.
package sidebar

import (
	"errors"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// DefaultWidth is the frame width, in terminal columns, when Options.Width
// is zero. It matches the approved mockup.
const DefaultWidth = 36

// Options configures a sidebar.
type Options struct {
	// Session is the name of the session this sidebar sits next to, for the
	// host's use. The row highlight comes from Agent.Own, which the backend
	// sets; the model does not compare names.
	Session string
	// Width is the frame width in columns; zero means DefaultWidth.
	Width int
	// Backend performs every side effect. It must not be nil.
	Backend Backend
}

// ErrNoBackend is returned by Run when Options.Backend is nil.
var ErrNoBackend = errors.New("sidebar: backend is required")

// New returns the sidebar as a Bubble Tea model. Callers that need their own
// program options build it with tea.NewProgram; Run is the common path.
func New(opts Options) tea.Model {
	if opts.Width <= 0 {
		opts.Width = DefaultWidth
	}
	home, _ := os.UserHomeDir()
	return newModel(opts, home)
}

// Run drives the sidebar in the alternate screen with mouse support and
// terminal focus reporting until the backend's Quit succeeds or the program
// is killed. Focus reporting is what recolours the frame when the keyboard
// moves between the sidebar and claude; every frame goes out as one
// synchronized update so that recolour never shows half-painted.
func Run(opts Options) error {
	if opts.Backend == nil {
		return ErrNoBackend
	}
	p := tea.NewProgram(
		New(opts),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
		tea.WithReportFocus(),
		tea.WithOutput(syncOutput{os.Stdout}),
	)
	_, err := p.Run()
	return err
}

// ShortenHome replaces home, when it is dir itself or one of its ancestors,
// with ~. A sibling such as /home/user2 for home /home/user is left alone.
func ShortenHome(dir, home string) string {
	if home == "" {
		return dir
	}
	if dir == home {
		return "~"
	}
	if strings.HasPrefix(dir, home+"/") {
		return "~" + dir[len(home):]
	}
	return dir
}
