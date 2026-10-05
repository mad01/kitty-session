package sidebar

import (
	"errors"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// errNameRequired is shown when the name prompt is submitted empty.
var errNameRequired = errors.New("name required")

// tmpNameLayout is the time part of a generated tmp session name, as ks tmp formats it.
const tmpNameLayout = "0102-1504"

// doneMsg is the outcome of a backend action run off the update loop.
type doneMsg struct {
	err     error
	status  string      // shown on success
	follow  string      // agent to put the cursor on after the refresh
	refresh bool        // reload the list on success
	quit    bool        // exit the program on success
	retry   *newAttempt // on failure, reopen the name prompt for this attempt
}

// newAttempt is a pending "new agent": the name, plus either the directory
// or the request for a fresh scratch directory.
type newAttempt struct {
	name string
	dir  string
	tmp  bool
}

// act runs call off the update loop and reports ok when it succeeds.
func act(call func() error, ok doneMsg) tea.Cmd {
	return func() tea.Msg {
		if err := call(); err != nil {
			return doneMsg{err: err}
		}
		return ok
	}
}

// applyDone folds an action's outcome into the model.
func (m model) applyDone(msg doneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		if msg.retry != nil {
			next, cmd := m.askName(*msg.retry)
			next.setError(msg.err)
			return next, cmd
		}
		m.setError(msg.err)
		return m, nil
	}
	if msg.quit {
		return m, tea.Quit
	}
	if msg.status != "" {
		m.setStatus(msg.status)
	}
	if msg.follow != "" {
		m.follow = msg.follow
	}
	if msg.refresh {
		return m, m.listCmd()
	}
	return m, nil
}

// focusCursor focuses the agent under the cursor.
func (m model) focusCursor() (tea.Model, tea.Cmd) {
	a, ok := m.cursorAgent()
	if !ok {
		return m, nil
	}
	backend := m.backend
	return m, act(func() error { return backend.Focus(a.Name) }, doneMsg{refresh: true})
}

// focusAgentWindow hands the keyboard to this tab's claude window.
func (m model) focusAgentWindow() (tea.Model, tea.Cmd) {
	return m, act(m.backend.FocusAgentWindow, doneMsg{})
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
	backend, oldName := m.backend, m.target
	return m, act(
		func() error { return backend.Rename(oldName, newName) },
		doneMsg{status: "renamed to " + newName, follow: newName, refresh: true},
	)
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
	verb := "deleted "
	if keep {
		verb = "closed "
	}
	backend, name := m.backend, m.target
	return m, act(
		func() error { return backend.Close(name, keep) },
		doneMsg{status: verb + name, refresh: true},
	)
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
	backend, name := m.backend, m.trashed[m.trashIdx]
	return m, act(
		func() error { return backend.Restore(name) },
		doneMsg{status: "restored " + name, follow: name, refresh: true},
	)
}

// openPicker shows the repo picker and starts the repo scan.
func (m model) openPicker() (tea.Model, tea.Cmd) {
	m.mode = modePicker
	m.picker.reset()
	m.picker.input.Width = m.inputWidth()
	return m, tea.Batch(m.picker.input.Focus(), m.reposCmd())
}

// pickDir turns the chosen picker entry into a new-agent attempt. A tmp
// entry gets a time-stamped name like ks tmp; a repo without a suggested
// name goes to the name prompt.
func (m model) pickDir() (tea.Model, tea.Cmd) {
	item, ok := m.picker.selected()
	if !ok {
		return m, nil
	}
	if item.tmp {
		return m.createAgent(newAttempt{name: "tmp-" + m.now().Format(tmpNameLayout), tmp: true})
	}
	name := m.backend.SuggestName(item.path)
	if name == "" {
		return m.askName(newAttempt{dir: item.path})
	}
	return m.createAgent(newAttempt{name: name, dir: item.path})
}

// askName opens the name prompt for a pending attempt.
func (m model) askName(a newAttempt) (model, tea.Cmd) {
	m.mode = modeName
	m.pending = a
	m.picker.input.Blur()
	m.input.Width = m.inputWidth()
	return m, activate(&m.input, a.name)
}

// createAgent validates the name, then creates the session off the update
// loop. A failure reopens the name prompt with the error shown.
func (m model) createAgent(a newAttempt) (tea.Model, tea.Cmd) {
	a.name = strings.TrimSpace(a.name)
	if a.name == "" {
		m.setError(errNameRequired)
		return m, nil
	}
	m.mode = modeList
	m.input.Blur()
	m.picker.input.Blur()
	m.setStatus("creating " + a.name + ellipsis)
	return m, newCmd(m.backend, a)
}

// newCmd creates the session. The scratch directory for a tmp attempt is
// made right before New and removed again when New fails.
func newCmd(backend Backend, a newAttempt) tea.Cmd {
	return func() tea.Msg {
		dir := a.dir
		if a.tmp {
			var err error
			if dir, err = backend.TmpDir(); err != nil {
				return doneMsg{err: err, retry: &a}
			}
		}
		if err := backend.New(a.name, dir); err != nil {
			if a.tmp && dir != "" {
				_ = os.RemoveAll(dir)
			}
			return doneMsg{err: err, retry: &a}
		}
		return doneMsg{status: "created " + a.name, follow: a.name, refresh: true}
	}
}

func (m model) shellSplit() (tea.Model, tea.Cmd) {
	return m, act(m.backend.ShellSplit, doneMsg{})
}

func (m model) hooksStatus() (tea.Model, tea.Cmd) {
	backend := m.backend
	return m, func() tea.Msg {
		s, err := backend.HooksStatus()
		return doneMsg{status: s, err: err}
	}
}

// quit asks the backend to tear ks down and exits only once that succeeded.
func (m model) quit() (tea.Model, tea.Cmd) {
	return m, act(m.backend.Quit, doneMsg{quit: true})
}
