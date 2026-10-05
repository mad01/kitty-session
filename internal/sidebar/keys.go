package sidebar

import tea "github.com/charmbracelet/bubbletea"

// keyHelp is one row of the keys popup: the key and what it does.
type keyHelp struct {
	key    string
	action string
}

// keyHelps is the keys popup, top to bottom. The last two rows are the
// kitty chords that move the keyboard between the sidebar and claude.
var keyHelps = []keyHelp{
	{"j/k ↑↓", "move"},
	{"enter", "focus / reopen"},
	{"1-9", "jump"},
	{"l tab q", "to agent"},
	{"n", "new"},
	{"r", "rename"},
	{"c", "close keep"},
	{"d", "delete"},
	{"u", "restore"},
	{"/", "filter"},
	{"m", "menu"},
	{"?", "keys"},
	{"ctrl+b s", "sidebar"},
	{"ctrl+b a", "agent"},
}

// keyColumn is the width of the key column in the keys popup.
const keyColumn = 10

// keysLines renders the two-column table the keys popup shows.
func keysLines() []string {
	lines := make([]string, len(keyHelps))
	for i, k := range keyHelps {
		lines[i] = fitWidth(k.key, keyColumn) + k.action
	}
	return lines
}

// openKeys shows the keys popup.
func (m model) openKeys() (tea.Model, tea.Cmd) {
	m.mode = modeKeys
	return m, nil
}

// updateKeys closes the popup on esc, ? or q and ignores everything else.
func (m model) updateKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "?", "q":
		m.mode = modeList
	}
	return m, nil
}

// keysPopup draws the keys table in a popup.
func (m model) keysPopup(inner int) []string {
	return renderPopup(keysLines(), -1, inner)
}
