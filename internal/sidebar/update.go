package sidebar

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.picker.input.Width = m.inputWidth()
		m.filter.Width = m.inputWidth()
		m.clampCursor()
		if msg.Width == m.pinned {
			return m, nil // already pinned at this width
		}
		return m, m.pinCmd(msg.Width)
	case pinMsg:
		if msg.err != nil {
			m.setError(msg.err)
			return m, nil
		}
		m.pinned = msg.cols
		return m, nil
	case doneMsg:
		return m.applyDone(msg)
	case tickMsg:
		return m, tea.Batch(m.listCmd(), tickCmd())
	case animMsg:
		m.frame++
		return m, animCmd()
	case agentsMsg:
		return m.applyAgents(msg), nil
	case reposMsg:
		m.picker.setRepos(msg.repos, msg.err)
		return m, nil
	case tea.FocusMsg:
		m.focused, m.focusSeen = true, true
		return m, nil
	case tea.BlurMsg:
		m.focused, m.focusSeen = false, true
		return m, nil
	case focusMsg:
		return m.applyFocusSeed(msg), nil
	case tea.MouseMsg:
		return m.updateMouse(msg)
	case tea.KeyMsg:
		return m.updateKey(msg)
	}
	return m, nil
}

// applyFocusSeed applies the startup focus snapshot unless a focus event has
// already arrived, which is newer by definition.
func (m model) applyFocusSeed(msg focusMsg) model {
	if m.focusSeen {
		return m
	}
	if msg.err != nil {
		m.setError(msg.err)
		return m
	}
	m.focused = msg.focused
	return m
}

// updateKey clears the transient status, then routes the key by mode.
// Ctrl-C is swallowed everywhere: the sidebar never exits on its own.
func (m model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.status = ""
	if msg.Type == tea.KeyCtrlC {
		return m, nil
	}
	switch m.mode {
	case modeFilter:
		return m.updateFilter(msg)
	case modeMenu:
		return m.updateMenu(msg)
	case modePicker:
		return m.updatePicker(msg)
	case modeName:
		return m.updateName(msg)
	case modeKind:
		return m.updateKind(msg)
	case modeRename:
		return m.updateRename(msg)
	case modeConfirm:
		return m.updateConfirm(msg)
	case modeRestore:
		return m.updateRestore(msg)
	case modeKeys:
		return m.updateKeys(msg)
	default:
		return m.updateList(msg)
	}
}

func (m model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if n, ok := digitKey(msg); ok {
		return m.jumpTo(n - 1)
	}
	switch msg.String() {
	case "j", "down":
		m.moveCursor(1)
	case "k", "up":
		m.moveCursor(-1)
	case "enter":
		return m.focusCursor()
	case "l", "tab", "q":
		return m.focusAgentWindow()
	case "/":
		m.mode = modeFilter
		m.filter.Width = m.inputWidth()
		return m, m.filter.Focus()
	case "esc":
		m.filter.SetValue("")
		m.clampCursor()
	case "n":
		return m.openPicker()
	case "r":
		return m.startRename()
	case "c":
		return m.startConfirm(actionClose)
	case "d":
		return m.startConfirm(actionDelete)
	case "u":
		return m.openRestore()
	case "m":
		m.mode = modeMenu
		m.menu.cursor = 0
	case "?":
		return m.openKeys()
	}
	return m, nil
}

// digitKey reports the digit 1..9 a key carries, if any.
func digitKey(msg tea.KeyMsg) (int, bool) {
	if msg.Type != tea.KeyRunes || len(msg.Runes) != 1 {
		return 0, false
	}
	r := msg.Runes[0]
	if r < '1' || r > '9' {
		return 0, false
	}
	return int(r - '0'), true
}

// jumpTo moves the cursor to the row at index i and focuses that agent.
func (m model) jumpTo(i int) (tea.Model, tea.Cmd) {
	if i >= len(m.visible()) {
		return m, nil
	}
	m.cursor = i
	m.navigated = true
	return m.focusCursor()
}

// updateFilter edits the `/` filter. Enter keeps it, esc clears it.
func (m model) updateFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.mode = modeList
		m.filter.Blur()
		m.clampCursor()
		return m, nil
	case "esc":
		m.mode = modeList
		m.filter.Blur()
		m.filter.SetValue("")
		m.clampCursor()
		return m, nil
	}
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	m.clampCursor()
	return m, cmd
}

// updateRename edits the cursor row's name in place.
func (m model) updateRename(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		m.input.Blur()
		return m, nil
	case "enter":
		return m.finishRename()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		return m.runConfirm()
	case "n", "N", "esc":
		m.mode = modeList
	}
	return m, nil
}

func (m model) updateRestore(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		m.trashIdx = clamp(m.trashIdx+1, len(m.trashed))
	case "k", "up":
		m.trashIdx = clamp(m.trashIdx-1, len(m.trashed))
	case "enter":
		return m.finishRestore()
	case "esc":
		m.mode = modeList
	}
	return m, nil
}

// updateName edits the name of a new agent after the picker. Enter takes a
// non-empty name on to the kind chooser.
func (m model) updateName(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modePicker
		m.input.Blur()
		return m, m.picker.input.Focus()
	case "enter":
		a := m.pending
		a.name = strings.TrimSpace(m.input.Value())
		if a.name == "" {
			m.setError(errNameRequired)
			return m, nil
		}
		return m.askKind(a)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// updateMouse handles left clicks: footer labels, rows, and menu entries.
func (m model) updateMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	m.status = ""
	line := msg.Y - 1 // content line index; the top border is y 0
	switch m.mode {
	case modeMenu:
		return m.clickMenu(msg.X, line)
	case modeList:
		return m.clickList(msg.X, line)
	case modeKeys:
		m.mode = modeList // any click dismisses the keys popup
	}
	return m, nil
}

// clickList dispatches a click in list mode: the footer's halves open the
// picker and the menu, a row click focuses that agent.
func (m model) clickList(x, line int) (tea.Model, tea.Cmd) {
	if line == m.innerHeight()-1 {
		if x < m.frameWidth()/2 {
			return m.openPicker()
		}
		m.mode = modeMenu
		m.menu.cursor = 0
		return m, nil
	}
	if line < bodyStart {
		return m, nil
	}
	row := (line - bodyStart) / rowHeight
	idx := m.rowOffset() + row
	if idx >= len(m.visible()) || row >= m.rowsFit() {
		return m, nil
	}
	m.cursor = idx
	m.navigated = true
	return m.focusCursor()
}

// activate focuses a text input and places the cursor at its end.
func activate(ti *textinput.Model, value string) tea.Cmd {
	ti.SetValue(value)
	ti.CursorEnd()
	return ti.Focus()
}

// inputWidth is the room a text input has on a row line.
func (m model) inputWidth() int {
	w := m.innerWidth() - rowIndent - edgePad
	if w < 1 {
		return 1
	}
	return w
}

// clamp keeps i within [0, n).
func clamp(i, n int) int {
	if i >= n {
		i = n - 1
	}
	if i < 0 {
		i = 0
	}
	return i
}
