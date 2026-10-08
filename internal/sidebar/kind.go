package sidebar

import tea "github.com/charmbracelet/bubbletea"

// kindOptions are the agent kinds the chooser offers, in display order, as
// Backend.New takes them: the session.Agent* values. claude comes first and
// is the default.
var kindOptions = []string{"claude", "pi", "shell"}

// kindBadge is the short label a row shows after its name for a kind other
// than claude: pi as is, shell as sh. Claude rows, and rows from a backend
// that reports no kind, show none.
func kindBadge(kind string) string {
	switch kind {
	case "", "claude":
		return ""
	case "shell":
		return "sh"
	default:
		return kind
	}
}

// updateKind moves the kind chooser's cursor, creates the pending agent
// with the chosen kind on enter, and goes back to the name prompt on esc.
func (m model) updateKind(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		m.kindIdx = clamp(m.kindIdx+1, len(kindOptions))
	case "k", "up":
		m.kindIdx = clamp(m.kindIdx-1, len(kindOptions))
	case "enter":
		a := m.pending
		a.kind = kindOptions[m.kindIdx]
		return m.createAgent(a)
	case "esc":
		a := m.pending
		a.kind = kindOptions[m.kindIdx]
		return m.askName(a)
	}
	return m, nil
}

// kindPopup draws the kind chooser: a title, then one line per kind with
// the cursor's highlighted.
func (m model) kindPopup(inner int) []string {
	lines := append([]string{popupHintStyle.Render("agent")}, kindOptions...)
	return renderPopup(lines, m.kindIdx+1, inner)
}
