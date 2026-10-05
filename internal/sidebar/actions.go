package sidebar

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// focusCursor focuses the agent under the cursor.
func (m model) focusCursor() (tea.Model, tea.Cmd) {
	a, ok := m.cursorAgent()
	if !ok {
		return m, nil
	}
	if err := m.backend.Focus(a.Name); err != nil {
		m.setError(err)
		return m, nil
	}
	return m, m.listCmd()
}

// focusAgentWindow hands the keyboard to this tab's claude window.
func (m model) focusAgentWindow() (tea.Model, tea.Cmd) {
	if err := m.backend.FocusAgentWindow(); err != nil {
		m.setError(err)
	}
	return m, nil
}

func (m model) startRename() (tea.Model, tea.Cmd) {
	a, ok := m.cursorAgent()
	if !ok {
		return m, nil
	}
	m.mode = modeRename
	m.target = a.Name
	m.input.Width = m.inputWidth()
	return m, activate(&m.input, a.Name)
}

func (m model) finishRename() (tea.Model, tea.Cmd) {
	newName := strings.TrimSpace(m.input.Value())
	m.mode = modeList
	m.input.Blur()
	if newName == "" || newName == m.target {
		return m, nil
	}
	if err := m.backend.Rename(m.target, newName); err != nil {
		m.setError(err)
		return m, nil
	}
	m.follow = newName
	m.setStatus("renamed to " + newName)
	return m, m.listCmd()
}

func (m model) startConfirm(action confirmAction) (tea.Model, tea.Cmd) {
	a, ok := m.cursorAgent()
	if !ok {
		return m, nil
	}
	m.mode = modeConfirm
	m.confirm = action
	m.target = a.Name
	return m, nil
}

// runConfirm performs the confirmed close or delete.
func (m model) runConfirm() (tea.Model, tea.Cmd) {
	m.mode = modeList
	keep := m.confirm == actionClose
	if err := m.backend.Close(m.target, keep); err != nil {
		m.setError(err)
		return m, nil
	}
	if keep {
		m.setStatus("closed " + m.target)
	} else {
		m.setStatus("deleted " + m.target)
	}
	return m, m.listCmd()
}

func (m model) openRestore() (tea.Model, tea.Cmd) {
	names, err := m.backend.Trashed()
	if err != nil {
		m.setError(err)
		return m, nil
	}
	if len(names) == 0 {
		m.setStatus("nothing to restore")
		return m, nil
	}
	m.trashed = names
	m.trashIdx = 0
	m.mode = modeRestore
	return m, nil
}

func (m model) finishRestore() (tea.Model, tea.Cmd) {
	m.mode = modeList
	if m.trashIdx < 0 || m.trashIdx >= len(m.trashed) {
		return m, nil
	}
	name := m.trashed[m.trashIdx]
	if err := m.backend.Restore(name); err != nil {
		m.setError(err)
		return m, nil
	}
	m.follow = name
	m.setStatus("restored " + name)
	return m, m.listCmd()
}

// openPicker shows the repo picker and starts the repo scan.
func (m model) openPicker() (tea.Model, tea.Cmd) {
	m.mode = modePicker
	m.picker.reset()
	m.picker.input.Width = m.inputWidth()
	return m, tea.Batch(m.picker.input.Focus(), m.reposCmd())
}

// pickDir resolves the chosen picker entry to a directory and creates the
// agent, or asks for a name when the backend has no suggestion.
func (m model) pickDir() (tea.Model, tea.Cmd) {
	item, ok := m.picker.selected()
	if !ok {
		return m, nil
	}
	dir := item.path
	if item.tmp {
		var err error
		if dir, err = m.backend.TmpDir(); err != nil {
			m.setError(err)
			return m, nil
		}
	}
	name := m.backend.SuggestName(dir)
	if name == "" {
		return m.askName("", dir)
	}
	return m.createAgent(name, dir)
}

// askName opens the name prompt for a new agent rooted at dir.
func (m model) askName(name, dir string) (model, tea.Cmd) {
	m.mode = modeName
	m.newDir = dir
	m.input.Width = m.inputWidth()
	return m, activate(&m.input, name)
}

// createAgent asks the backend for a new session; on failure it falls back
// to the name prompt with the error shown so the user can pick another name.
func (m model) createAgent(name, dir string) (tea.Model, tea.Cmd) {
	name = strings.TrimSpace(name)
	if err := m.backend.New(name, dir); err != nil {
		next, cmd := m.askName(name, dir)
		next.setError(err)
		return next, cmd
	}
	m.mode = modeList
	m.input.Blur()
	m.follow = name
	m.setStatus("created " + name)
	return m, m.listCmd()
}

func (m model) shellSplit() (tea.Model, tea.Cmd) {
	if err := m.backend.ShellSplit(); err != nil {
		m.setError(err)
	}
	return m, nil
}

func (m model) hooksStatus() (tea.Model, tea.Cmd) {
	s, err := m.backend.HooksStatus()
	if err != nil {
		m.setError(err)
		return m, nil
	}
	m.setStatus(s)
	return m, nil
}

// quit asks the backend to tear ks down and exits only once that succeeded.
func (m model) quit() (tea.Model, tea.Cmd) {
	if err := m.backend.Quit(); err != nil {
		m.setError(err)
		return m, nil
	}
	return m, tea.Quit
}
