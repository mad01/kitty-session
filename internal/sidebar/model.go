package sidebar

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// mode is what the keyboard currently drives.
type mode int

const (
	modeList    mode = iota // the agent list
	modeFilter              // typing into the name filter
	modeMenu                // the footer menu popup
	modePicker              // the repo picker for a new agent
	modeName                // naming a new agent after the picker
	modeRename              // inline rename of the cursor row
	modeConfirm             // y/n before close or delete
	modeRestore             // picking a trashed session to restore
)

// Layout constants, in terminal cells and lines.
const (
	frameCells    = 2             // left and right border
	frameLines    = 2             // top and bottom border
	rowHeight     = 2             // name line plus title line
	rowIndent     = 3             // marker, dot, space before the name; title aligns under the name
	edgePad       = 1             // blank cell between content and the right border
	bodyStart     = 2             // content lines above the rows: header and filter line
	listChrome    = bodyStart + 2 // plus the status line and footer below them
	defaultHeight = 24
	inputLimit    = 128
	minTermWidth  = 8
)

// Timings.
const (
	pollInterval = 3 * time.Second
	animInterval = 350 * time.Millisecond
)

// Messages the model sends itself.
type (
	tickMsg   time.Time
	animMsg   time.Time
	agentsMsg struct {
		gen    int // listGen when the List was issued; older results are dropped
		agents []Agent
		err    error
	}
	pinMsg struct {
		err error
	}
	reposMsg struct {
		repos []Repo
		err   error
	}
)

// confirmAction is what a y/n prompt will do.
type confirmAction int

const (
	actionClose confirmAction = iota
	actionDelete
)

type model struct {
	opts    Options
	backend Backend
	home    string
	now     func() time.Time

	agents  []Agent // sorted by priority
	listGen int     // generation of the latest List issued
	cursor  int     // index into visible()
	width   int     // terminal size from the last WindowSizeMsg; zero before it
	height  int
	mode    mode
	frame   int // animation frame for pulsing dots

	filter textinput.Model // the `/` name filter
	input  textinput.Model // rename and new-agent name
	menu   menu
	picker picker

	confirm  confirmAction
	target   string     // agent the confirm, rename or restore acts on
	pending  newAttempt // new agent awaiting a name
	trashed  []string
	trashIdx int
	follow   string // agent to put the cursor on after the next List
	// navigated is set by a cursor key, digit or row click and cleared by
	// snapToOwn. While it is clear, every refresh keeps the cursor on the
	// Own row, so the per-tab sidebars read as one sidebar whose cursor
	// sits on the tab you are in.
	navigated bool

	status    string // transient line above the footer; cleared on the next key
	statusErr bool
}

func newModel(opts Options, home string) model {
	return model{
		opts:    opts,
		backend: opts.Backend,
		home:    home,
		now:     time.Now,
		filter:  newInput(""),
		input:   newInput(""),
		picker:  newPicker(),
	}
}

func newInput(prompt string) textinput.Model {
	ti := textinput.New()
	ti.Prompt = prompt
	ti.PromptStyle = filterPromptStyle
	ti.TextStyle = inputStyle
	ti.Cursor.Style = inputCursorStyle
	ti.CharLimit = inputLimit
	return ti
}

// Init starts the poll and animation tickers; the first tick loads the list.
func (m model) Init() tea.Cmd {
	return tea.Batch(func() tea.Msg { return tickMsg(time.Now()) }, animCmd())
}

func tickCmd() tea.Cmd {
	return tea.Tick(pollInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func animCmd() tea.Cmd {
	return tea.Tick(animInterval, func(t time.Time) tea.Msg { return animMsg(t) })
}

// listCmd asks the backend for the agents off the update loop. Each call
// opens a new generation so a slow older result cannot overwrite a newer one.
func (m *model) listCmd() tea.Cmd {
	m.listGen++
	gen, backend := m.listGen, m.backend
	return func() tea.Msg {
		agents, err := backend.List()
		return agentsMsg{gen: gen, agents: agents, err: err}
	}
}

// reposCmd scans repositories off the update loop; the walk can take a while.
func (m model) reposCmd() tea.Cmd {
	backend := m.backend
	return func() tea.Msg {
		repos, err := backend.Repos()
		return reposMsg{repos: repos, err: err}
	}
}

// pinCmd reports the terminal width to the host.
func (m model) pinCmd(cols int) tea.Cmd {
	backend := m.backend
	return func() tea.Msg {
		return pinMsg{err: backend.PinWidth(cols)}
	}
}

// applyAgents replaces the list with a freshly sorted copy. The cursor goes
// to the agent a pending follow names, else stays on the agent the user
// navigated to, else snaps to the Own row. Results from a List older than
// the latest issued one are dropped.
func (m model) applyAgents(msg agentsMsg) model {
	if msg.gen < m.listGen {
		return m
	}
	if msg.err != nil {
		m.setError(msg.err)
		return m
	}
	keep := m.cursorName()
	if m.follow != "" {
		keep, m.follow = m.follow, ""
		m.navigated = true
	}
	m.agents = make([]Agent, len(msg.agents))
	copy(m.agents, msg.agents)
	sortAgents(m.agents)
	if i := m.indexOf(keep); m.navigated && i >= 0 {
		m.cursor = i
		return m
	}
	m.snapToOwn()
	return m
}

// snapToOwn puts the cursor on the Own row, or row 0 when no agent is Own,
// and forgets any navigation so later refreshes keep it there.
func (m *model) snapToOwn() {
	m.cursor = max(m.ownIndex(), 0)
	m.navigated = false
}

// ownIndex returns the position of the Own agent among the visible rows, or -1.
func (m model) ownIndex() int {
	for i, a := range m.visible() {
		if a.Own {
			return i
		}
	}
	return -1
}

// indexOf returns the position of the named agent among the visible rows, or -1.
func (m model) indexOf(name string) int {
	if name == "" {
		return -1
	}
	for i, a := range m.visible() {
		if a.Name == name {
			return i
		}
	}
	return -1
}

// visible returns the agents that pass the name filter, in display order.
func (m model) visible() []Agent {
	needle := strings.ToLower(m.filter.Value())
	if needle == "" {
		return m.agents
	}
	var out []Agent
	for _, a := range m.agents {
		if strings.Contains(strings.ToLower(a.Name), needle) {
			out = append(out, a)
		}
	}
	return out
}

// cursorAgent returns the agent under the cursor.
func (m model) cursorAgent() (Agent, bool) {
	v := m.visible()
	if m.cursor < 0 || m.cursor >= len(v) {
		return Agent{}, false
	}
	return v[m.cursor], true
}

func (m model) cursorName() string {
	a, ok := m.cursorAgent()
	if !ok {
		return ""
	}
	return a.Name
}

// moveCursor shifts the cursor by delta rows and records the navigation.
func (m *model) moveCursor(delta int) {
	m.cursor += delta
	m.navigated = true
	m.clampCursor()
}

func (m *model) clampCursor() {
	n := len(m.visible())
	if m.cursor >= n {
		m.cursor = n - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// setStatus shows the first line of s above the footer until the next key.
func (m *model) setStatus(s string) {
	m.status, m.statusErr = firstLine(s), false
}

// setError shows the first line of err in the error color until the next key.
func (m *model) setError(err error) {
	m.status, m.statusErr = firstLine(err.Error()), true
}

// firstLine returns s up to its first newline, trimmed.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// frameWidth is the width the frame renders at: the configured width, or
// the terminal width when the terminal is narrower so lines never wrap.
func (m model) frameWidth() int {
	w := m.opts.Width
	if m.width > 0 && m.width < w {
		w = m.width
	}
	if w < minTermWidth {
		w = minTermWidth
	}
	return w
}

// frameHeight is the terminal height, or a default before the first resize.
func (m model) frameHeight() int {
	if m.height <= 0 {
		return defaultHeight
	}
	return m.height
}

// innerWidth is the number of cells between the two border columns.
func (m model) innerWidth() int {
	return m.frameWidth() - frameCells
}

// innerHeight is the number of content lines between the two border rows.
func (m model) innerHeight() int {
	return m.frameHeight() - frameLines
}

// rowsFit is how many two-line rows the body can show.
func (m model) rowsFit() int {
	body := m.innerHeight() - listChrome
	if body < rowHeight {
		return 0
	}
	return body / rowHeight
}

// rowOffset is the index of the first row drawn, chosen so the cursor row is on screen.
func (m model) rowOffset() int {
	fit := m.rowsFit()
	if fit <= 0 || m.cursor < fit {
		return 0
	}
	return m.cursor - fit + 1
}
