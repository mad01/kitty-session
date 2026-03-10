package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mad01/kitty-session/internal/session"
)

type mode int

const (
	modeList mode = iota
	modeRepoPicker
	modeInput
	modeRename
	modeConfirm
	modeHelp
	modeQuitConfirm
	modeRestore
)

type confirmAction int

const (
	actionClose confirmAction = iota
	actionDelete
)

type model struct {
	list          list.Model
	repoList      list.Model
	trashList     list.Model
	helpViewport  viewport.Model
	store         *session.Store
	keys          *delegateKeyMap
	mode          mode
	width         int
	height        int
	textInput     textinput.Model
	repoDir       string
	confirmAction confirmAction
	confirmItem   sessionItem
	err           error
	quitting      bool
}

func newModel(store *session.Store, items []sessionItem, repoItems []repoItem) model {
	delegate := newItemDelegate()
	keys := newDelegateKeyMap()

	listItems := make([]list.Item, len(items))
	for i, item := range items {
		listItems[i] = item
	}

	l := list.New(listItems, delegate, 60, 18)
	l.Styles.StatusBar = statusBarStyle
	l.Styles.FilterPrompt = filterPromptStyle
	l.Styles.FilterCursor = filterCursorStyle
	l.Styles.NoItems = noItemsStyle
	l.Styles.HelpStyle = helpStyle
	l.Styles.PaginationStyle = paginationStyle
	l.Styles.ActivePaginationDot = activeDotStyle
	l.Styles.InactivePaginationDot = inactiveDotStyle
	l.SetShowStatusBar(false)
	l.SetShowTitle(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(true)
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return keys.ShortHelp()
	}
	l.AdditionalFullHelpKeys = func() []key.Binding {
		return []key.Binding{keys.open, keys.newSes, keys.close, keys.delete}
	}

	// Build the repo picker list
	rItems := make([]list.Item, len(repoItems))
	for i, r := range repoItems {
		rItems[i] = r
	}
	rl := list.New(rItems, repoDelegate{}, 60, 18)
	rl.Styles.StatusBar = statusBarStyle
	rl.Styles.FilterPrompt = filterPromptStyle
	rl.Styles.FilterCursor = filterCursorStyle
	rl.Styles.NoItems = noItemsStyle
	rl.Styles.HelpStyle = helpStyle
	rl.Styles.PaginationStyle = paginationStyle
	rl.Styles.ActivePaginationDot = activeDotStyle
	rl.Styles.InactivePaginationDot = inactiveDotStyle
	rl.SetShowStatusBar(false)
	rl.SetShowTitle(false)
	rl.SetShowHelp(false)
	rl.SetFilteringEnabled(true)

	ti := textinput.New()
	ti.Prompt = ""
	ti.TextStyle = inputStyle
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(colorAccent)
	ti.CharLimit = 128

	return model{
		list:      l,
		repoList:  rl,
		textInput: ti,
		store: store,
		keys:  keys,
		mode:  modeList,
	}
}

// activateTextInput prepares the text input with a value and focuses it.
func (m *model) activateTextInput(value string) {
	m.textInput.SetValue(value)
	m.textInput.CursorEnd()
	m.textInput.Focus()
	m.err = nil
}

// innerSize returns the content width and height inside the frame.
func (m model) innerSize() (int, int) {
	// Frame border (2) + padding (4) + centering margin (2)
	w := m.width - 8
	h := m.height - 6
	if w > 150 {
		w = 150
	}
	if h > 50 {
		h = 50
	}
	if w < 40 {
		w = 40
	}
	if h < 10 {
		h = 10
	}
	return w, h
}

// tickMsg triggers a periodic refresh of session states.
type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// animTickMsg drives the pulsing animation for the working badge.
type animTickMsg time.Time

func animTickCmd() tea.Cmd {
	return tea.Tick(350*time.Millisecond, func(t time.Time) tea.Msg {
		return animTickMsg(t)
	})
}

func (m model) Init() tea.Cmd {
	return tea.Batch(tickCmd(), animTickCmd())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		w, h := m.innerSize()
		m.list.SetWidth(w)
		m.list.SetHeight(h - 3) // reserve lines for frame overhead (title section + dividers + help)
		m.repoList.SetWidth(w)
		m.repoList.SetHeight(h - 3)
		if m.mode == modeRestore {
			m.trashList.SetWidth(w)
			m.trashList.SetHeight(h - 3)
		}
	case animTickMsg:
		animFrame = (animFrame + 1) % len(workingPulseColors)
		return m, animTickCmd()
	case tickMsg:
		if m.mode == modeList {
			m.refreshList()
		}
		return m, tickCmd()
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC || msg.Type == tea.KeyCtrlD {
			return m, m.list.NewStatusMessage(helpBarStyle.Render("press ") +
				helpKeyInlineStyle.Render("esc") + helpBarStyle.Render(" or ") +
				helpKeyInlineStyle.Render("q") + helpBarStyle.Render(" to quit"))
		}
	}

	switch m.mode {
	case modeRepoPicker:
		return m.updateRepoPicker(msg)
	case modeInput:
		return m.updateInput(msg)
	case modeRename:
		return m.updateRename(msg)
	case modeConfirm:
		return m.updateConfirm(msg)
	case modeHelp:
		return m.updateHelp(msg)
	case modeRestore:
		return m.updateRestore(msg)
	case modeQuitConfirm:
		return m.updateQuitConfirm(msg)
	default:
		return m.updateList(msg)
	}
}

func (m model) updateList(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Don't intercept keys when filtering
		if m.list.FilterState() == list.Filtering {
			break
		}

		switch {
		case msg.String() == "q":
			m.mode = modeQuitConfirm
			return m, nil
		case msg.Type == tea.KeyEscape && m.list.FilterState() == list.Unfiltered:
			m.mode = modeQuitConfirm
			return m, nil
		case msg.String() == "?":
			m.mode = modeHelp
			w, h := m.innerSize()
			m.helpViewport = viewport.New(w, h)
			m.helpViewport.SetContent(m.renderHelp())
			return m, nil
		case key.Matches(msg, m.keys.open):
			return m.handleOpen()
		case key.Matches(msg, m.keys.newSes):
			m.mode = modeRepoPicker
			m.repoList.ResetFilter()
			m.err = nil
			return m, nil
		case key.Matches(msg, m.keys.rename):
			return m.startRename()
		case key.Matches(msg, m.keys.close):
			return m.startConfirm(actionClose)
		case key.Matches(msg, m.keys.delete):
			return m.startConfirm(actionDelete)
		case key.Matches(msg, m.keys.restore):
			return m.startRestore()
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m model) updateRepoPicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case msg.Type == tea.KeyEnter:
			item, ok := m.repoList.SelectedItem().(repoItem)
			if !ok {
				return m, nil
			}
			return m.handleRepoSelect(item)
		case msg.Type == tea.KeyEscape:
			if m.repoList.FilterState() == list.Filtering || m.repoList.FilterState() == list.FilterApplied {
				m.repoList.ResetFilter()
				return m, nil
			}
			m.mode = modeList
			return m, nil
		default:
			// When already filtering, let the list handle navigation keys
			if m.repoList.FilterState() == list.Filtering {
				break
			}
			if msg.Type == tea.KeyRunes {
				// Auto-enter filter mode when typing (/ triggers it natively)
				if !(len(msg.Runes) == 1 && msg.Runes[0] == '/') {
					slash := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}}
					m.repoList, _ = m.repoList.Update(slash)
				}
			}
		}
	}

	var cmd tea.Cmd
	m.repoList, cmd = m.repoList.Update(msg)
	return m, cmd
}

// handleRepoSelect creates a session directly from the selected repo,
// falling back to the name input if the auto-generated name conflicts.
func (m model) handleRepoSelect(item repoItem) (tea.Model, tea.Cmd) {
	dir := item.path

	if item.isTmp {
		tmpDir, err := os.MkdirTemp("", "ks-*")
		if err != nil {
			m.mode = modeList
			m.repoList.ResetFilter()
			return m, m.list.NewStatusMessage(errorStyle.Render(err.Error()))
		}
		dir = tmpDir
	}

	name := suggestSessionNameForDir(dir)

	if name != "" && !m.store.Exists(name) {
		if err := createSession(name, dir, m.store); err != nil {
			m.mode = modeList
			m.repoList.ResetFilter()
			return m, m.list.NewStatusMessage(errorStyle.Render(err.Error()))
		}
		m.refreshList()
		m.mode = modeList
		m.repoList.ResetFilter()
		return m, m.list.NewStatusMessage(fmt.Sprintf("created %q", name))
	}

	// Name conflicts or is empty — let user edit it
	m.repoDir = dir
	m.mode = modeInput
	m.activateTextInput(name)
	if name != "" {
		m.err = fmt.Errorf("session %q already exists — pick a different name", name)
	}
	return m, nil
}

func (m model) updateInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.Type {
		case tea.KeyEscape:
			m.textInput.Blur()
			m.mode = modeRepoPicker
			return m, nil
		case tea.KeyEnter:
			return m.handleCreate()
		}
	}
	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m model) updateConfirm(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "y", "Y":
			return m.executeConfirm()
		case "n", "N", "esc":
			m.mode = modeList
			return m, nil
		}
	}
	return m, nil
}

func (m model) updateQuitConfirm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.Type {
		case tea.KeyEnter:
			m.quitting = true
			return m, tea.Quit
		case tea.KeyEscape:
			m.mode = modeList
			return m, nil
		}
	}
	return m, nil
}

func (m model) updateHelp(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.String() {
		case "?", "esc", "q":
			m.mode = modeList
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.helpViewport, cmd = m.helpViewport.Update(msg)
	return m, cmd
}

func (m model) startRestore() (tea.Model, tea.Cmd) {
	items, err := loadTrashedSessions(m.store)
	if err != nil || len(items) == 0 {
		return m, m.list.NewStatusMessage(helpBarStyle.Render("no deleted sessions to restore"))
	}

	listItems := make([]list.Item, len(items))
	for i, item := range items {
		listItems[i] = item
	}

	w, h := m.innerSize()
	tl := list.New(listItems, newItemDelegate(), w, h-3)
	tl.Styles.StatusBar = statusBarStyle
	tl.Styles.FilterPrompt = filterPromptStyle
	tl.Styles.FilterCursor = filterCursorStyle
	tl.Styles.NoItems = noItemsStyle
	tl.Styles.HelpStyle = helpStyle
	tl.Styles.PaginationStyle = paginationStyle
	tl.Styles.ActivePaginationDot = activeDotStyle
	tl.Styles.InactivePaginationDot = inactiveDotStyle
	tl.SetShowStatusBar(false)
	tl.SetShowTitle(false)
	tl.SetShowHelp(false)
	tl.SetFilteringEnabled(false)

	m.trashList = tl
	m.mode = modeRestore
	return m, nil
}

func (m model) updateRestore(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case msg.Type == tea.KeyEnter || msg.String() == "o":
			item, ok := m.trashList.SelectedItem().(sessionItem)
			if !ok {
				return m, nil
			}
			if err := restoreSession(item.session.Name, m.store); err != nil {
				m.mode = modeList
				return m, m.list.NewStatusMessage(errorStyle.Render(err.Error()))
			}
			m.refreshList()
			m.mode = modeList
			return m, m.list.NewStatusMessage(fmt.Sprintf("restored %q", item.session.Name))
		case msg.Type == tea.KeyEscape:
			m.mode = modeList
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.trashList, cmd = m.trashList.Update(msg)
	return m, cmd
}

func (m model) handleOpen() (tea.Model, tea.Cmd) {
	item, ok := m.list.SelectedItem().(sessionItem)
	if !ok {
		return m, nil
	}
	if err := openSession(item.session, m.store); err != nil {
		m.err = err
		return m, m.list.NewStatusMessage(errorStyle.Render(err.Error()))
	}
	m.refreshList()
	return m, m.list.NewStatusMessage(fmt.Sprintf("opened %q", item.session.Name))
}

func (m model) handleCreate() (tea.Model, tea.Cmd) {
	name := m.textInput.Value()
	if name == "" {
		m.err = fmt.Errorf("name cannot be empty")
		return m, nil
	}
	if m.store.Exists(name) {
		m.err = fmt.Errorf("session %q already exists", name)
		return m, nil
	}

	dir := m.repoDir
	if dir == "" {
		dir, _ = os.Getwd()
	}

	if err := createSession(name, dir, m.store); err != nil {
		m.err = err
		m.textInput.Blur()
		m.mode = modeList
		return m, m.list.NewStatusMessage(errorStyle.Render(err.Error()))
	}
	m.textInput.Blur()
	m.refreshList()
	m.mode = modeList
	return m, m.list.NewStatusMessage(fmt.Sprintf("created %q", name))
}

func (m model) startRename() (tea.Model, tea.Cmd) {
	item, ok := m.list.SelectedItem().(sessionItem)
	if !ok {
		return m, nil
	}
	m.mode = modeRename
	m.activateTextInput(item.session.Name)
	return m, nil
}

func (m model) updateRename(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.Type {
		case tea.KeyEscape:
			m.textInput.Blur()
			m.mode = modeList
			return m, nil
		case tea.KeyEnter:
			return m.handleRename()
		}
	}
	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m model) handleRename() (tea.Model, tea.Cmd) {
	item, ok := m.list.SelectedItem().(sessionItem)
	if !ok {
		return m, nil
	}
	newName := m.textInput.Value()
	oldName := item.session.Name
	if newName == "" {
		m.err = fmt.Errorf("name cannot be empty")
		return m, nil
	}
	if newName == oldName {
		m.textInput.Blur()
		m.mode = modeList
		return m, nil
	}
	if err := renameSession(item.session, newName, m.store); err != nil {
		m.err = err
		return m, nil
	}
	m.textInput.Blur()
	m.refreshList()
	m.mode = modeList
	return m, m.list.NewStatusMessage(fmt.Sprintf("renamed %q → %q", oldName, newName))
}

func (m model) renderTextPrompt(label string) string {
	w, h := m.innerSize()
	prompt := inputPromptStyle.Render(label) + m.textInput.View()
	if m.err != nil {
		prompt += "\n" + errorStyle.Render("  "+m.err.Error())
	}
	return lipgloss.Place(w, h-3, lipgloss.Center, lipgloss.Center, prompt)
}

func (m model) startConfirm(action confirmAction) (tea.Model, tea.Cmd) {
	item, ok := m.list.SelectedItem().(sessionItem)
	if !ok {
		return m, nil
	}
	m.mode = modeConfirm
	m.confirmAction = action
	m.confirmItem = item
	return m, nil
}

func (m model) executeConfirm() (tea.Model, tea.Cmd) {
	item := m.confirmItem
	var statusMsg string

	switch m.confirmAction {
	case actionClose:
		if err := closeSession(item.session); err != nil {
			m.mode = modeList
			return m, m.list.NewStatusMessage(errorStyle.Render(err.Error()))
		}
		statusMsg = fmt.Sprintf("session %q tab closed", item.session.Name)
	case actionDelete:
		if err := deleteSession(item.session, m.store); err != nil {
			m.mode = modeList
			return m, m.list.NewStatusMessage(errorStyle.Render(err.Error()))
		}
		statusMsg = fmt.Sprintf("session %q deleted", item.session.Name)
	}

	m.refreshList()
	m.mode = modeList
	return m, m.list.NewStatusMessage(statusMsg)
}

// refreshList reloads sessions from disk and updates the list items.
func (m *model) refreshList() {
	items, err := loadSessions(m.store)
	if err != nil {
		return
	}
	listItems := make([]list.Item, len(items))
	for i, item := range items {
		listItems[i] = item
	}
	m.list.SetItems(listItems)
}

// View renders the TUI inside a centered frame.
func (m model) View() string {
	if m.quitting {
		return ""
	}

	w, _ := m.innerSize()

	switch m.mode {
	case modeQuitConfirm:
		content := m.renderQuitDialog()
		framed := frameStyle.Width(w).Render(content)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, framed)
	case modeConfirm:
		content := m.renderConfirmDialog()
		framed := frameStyle.Width(w).Render(content)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, framed)
	case modeHelp:
		framed := frameStyle.Width(w).Render(m.helpViewport.View())
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, framed)
	default:
		var title, help string
		switch m.mode {
		case modeRepoPicker:
			title = titleBarStyle.Render("select repository")
			help = helpKeyInlineStyle.Render("type to filter · ↑/↓ navigate · enter select · esc back")
		case modeInput:
			title = titleBarStyle.Render("new session")
			help = helpKeyInlineStyle.Render("enter confirm · ←/→ move cursor · esc back")
		case modeRename:
			title = titleBarStyle.Render("rename session")
			help = helpKeyInlineStyle.Render("enter confirm · ←/→ move cursor · esc cancel")
		case modeRestore:
			title = titleBarStyle.Render("restore deleted session")
			help = helpKeyInlineStyle.Render("enter restore · esc back")
		default:
			title = titleBarStyle.Render("ks · kitty claude session manager")
			help = helpKeyInlineStyle.Render("↑/k up · ↓/j down · / filter · o open · n new · r rename · c close · d delete · u restore · ? help")
		}

		var body string
		switch m.mode {
		case modeRepoPicker:
			body = m.repoList.View()
		case modeInput:
			body = m.renderInputPrompt()
		case modeRename:
			body = m.renderTextPrompt("Rename: ")
		case modeRestore:
			body = m.trashList.View()
		default:
			body = m.list.View()
		}

		return m.renderFramedView(
			lipgloss.PlaceHorizontal(w, lipgloss.Center, title),
			lipgloss.PlaceHorizontal(w, lipgloss.Center, help),
			body,
		)
	}
}

// renderFramedView draws a rounded box around title, help and body.
func (m model) renderFramedView(titleRow, helpRow, body string) string {
	w, _ := m.innerSize()
	innerW := w + 4 // content width inside borders (2-char padding each side)

	bc := lipgloss.NewStyle().Foreground(colorAccent)
	border := bc.Render("│")
	hLine := strings.Repeat("─", innerW)
	blank := border + strings.Repeat(" ", innerW) + border

	wrapLine := func(line string) string {
		padded := "  " + lipgloss.PlaceHorizontal(w, lipgloss.Left, line) + "  "
		return border + padded + border
	}

	wrapBlock := func(content string) []string {
		var out []string
		for _, line := range strings.Split(content, "\n") {
			out = append(out, wrapLine(line))
		}
		return out
	}

	var lines []string
	// Top border
	lines = append(lines, bc.Render("╭"+hLine+"╮"))
	// Section 1: title
	lines = append(lines, blank)
	lines = append(lines, wrapLine(titleRow))
	lines = append(lines, blank)
	// Divider
	lines = append(lines, bc.Render("├"+hLine+"┤"))
	// Section 2: help/commands
	lines = append(lines, wrapLine(helpRow))
	// Divider
	lines = append(lines, bc.Render("├"+hLine+"┤"))
	// Section 3: body
	lines = append(lines, wrapBlock(body)...)
	lines = append(lines, blank)
	// Bottom border
	lines = append(lines, bc.Render("╰"+hLine+"╯"))

	box := strings.Join(lines, "\n")
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m model) renderInputPrompt() string {
	return m.renderTextPrompt("Session name: ")
}

// dialogSize returns the available content area inside frameStyle
// (which adds border=2 + padding=2 vertical, border=2 + padding=4 horizontal).
func (m model) dialogSize() (int, int) {
	w, h := m.innerSize()
	return w - 6, h - 4 // subtract frameStyle chrome
}

func (m model) renderConfirmDialog() string {
	w, h := m.dialogSize()
	item := m.confirmItem

	var title string
	if m.confirmAction == actionDelete {
		title = "Delete session?"
	} else {
		title = "Close session tab?"
	}

	status := stateBadge(item.state, true)

	dialog := quitDialogStyle.Render(
		confirmStyle.Render(title) + "\n\n" +
			selectedNameStyle.Render(item.session.Name) + "  " + status + "\n" +
			dirStyle.Render(shortenDir(item.session.Dir)) + "\n\n" +
			helpKeyInlineStyle.Render("y") + helpBarStyle.Render(" confirm  ·  ") +
			helpKeyInlineStyle.Render("n") + helpBarStyle.Render(" cancel"),
	)
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, dialog)
}

func (m model) renderQuitDialog() string {
	w, h := m.dialogSize()
	dialog := quitDialogStyle.Render(
		confirmStyle.Render("Quit ks?") + "\n\n" +
			helpKeyInlineStyle.Render("enter") + helpBarStyle.Render(" quit  ·  ") +
			helpKeyInlineStyle.Render("esc") + helpBarStyle.Render(" cancel"),
	)
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, dialog)
}

func (m model) renderHelp() string {
	helpRow := func(k, desc string) string {
		return helpKeyStyle.Render(k) + helpDescStyle.Render(desc)
	}

	title := helpTitleStyle.Render("ks — Kitty Claude Session Manager")

	nav := helpSectionStyle.Render("Navigation")
	navKeys := lipgloss.JoinVertical(lipgloss.Left,
		helpRow("j/k / ↑↓", "Move up/down"),
		helpRow("g / home", "Go to top"),
		helpRow("G / end", "Go to bottom"),
	)

	actions := helpSectionStyle.Render("Actions")
	actionKeys := lipgloss.JoinVertical(lipgloss.Left,
		helpRow("o / enter", "Open or focus session"),
		helpRow("n", "Create new session"),
		helpRow("r", "Rename session"),
		helpRow("c", "Close tab (keep session)"),
		helpRow("d", "Delete session"),
		helpRow("u", "Restore deleted session"),
	)

	filter := helpSectionStyle.Render("Filter")
	filterKeys := lipgloss.JoinVertical(lipgloss.Left,
		helpRow("/", "Start fuzzy search"),
		helpRow("esc", "Clear filter"),
	)

	general := helpSectionStyle.Render("General")
	generalKeys := lipgloss.JoinVertical(lipgloss.Left,
		helpRow("?", "Toggle this help"),
		helpRow("q", "Quit"),
	)

	return lipgloss.JoinVertical(lipgloss.Left,
		title, "",
		nav, navKeys, "",
		actions, actionKeys, "",
		filter, filterKeys, "",
		general, generalKeys,
		"",
		helpBarStyle.Render("Press ? or esc to return"),
	)
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9-]+`)

// sanitizeName lowercases, replaces non-alphanumeric chars with hyphens,
// and collapses/trims consecutive hyphens.
func sanitizeName(s string) string {
	s = strings.ToLower(s)
	s = nonAlnum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return s
}

// suggestSessionNameForDir returns a default session name based on the given
// directory and its git branch (if inside a git repo).
func suggestSessionNameForDir(dir string) string {
	if dir == "" {
		return ""
	}
	name := sanitizeName(filepath.Base(dir))

	out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err == nil {
		branch := strings.TrimSpace(string(out))
		if branch != "" {
			name += "-" + sanitizeName(branch)
		}
	}
	return name
}

// Run starts the TUI and blocks until it exits.
func Run() error {
	store, err := session.NewStore()
	if err != nil {
		return err
	}

	items, err := loadSessions(store)
	if err != nil {
		return err
	}

	repoItems := loadRepos()

	m := newModel(store, items, repoItems)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}
