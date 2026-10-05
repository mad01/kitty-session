package sidebar

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// popupPad is the blank cell on each side of a popup line.
const popupPad = 1

// menuEntry is one line of the footer menu and what it does.
type menuEntry struct {
	label string
	run   func(model) (tea.Model, tea.Cmd)
}

// menuEntries is the footer menu, top to bottom.
var menuEntries = []menuEntry{
	{"new agent", model.openPicker},
	{"rename", model.startRename},
	{"close (keep)", func(m model) (tea.Model, tea.Cmd) { return m.startConfirm(actionClose) }},
	{"delete", func(m model) (tea.Model, tea.Cmd) { return m.startConfirm(actionDelete) }},
	{"restore", model.openRestore},
	{"shell split", model.shellSplit},
	{"hooks status", model.hooksStatus},
	{"keys", model.openKeys},
	{"quit ks", model.quit},
}

// menu is the footer menu's cursor.
type menu struct {
	cursor int
}

func menuLabels() []string {
	labels := make([]string, len(menuEntries))
	for i, e := range menuEntries {
		labels[i] = e.label
	}
	return labels
}

func (m model) updateMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		m.menu.cursor = clamp(m.menu.cursor+1, len(menuEntries))
	case "k", "up":
		m.menu.cursor = clamp(m.menu.cursor-1, len(menuEntries))
	case "enter":
		return m.runMenu(m.menu.cursor)
	case "esc", "m", "q":
		m.mode = modeList
	}
	return m, nil
}

// runMenu closes the menu and runs entry i.
func (m model) runMenu(i int) (tea.Model, tea.Cmd) {
	m.mode = modeList
	if i < 0 || i >= len(menuEntries) {
		return m, nil
	}
	return menuEntries[i].run(m)
}

// clickMenu runs the entry under a click at terminal column x and content
// line `line`, or closes the menu when the click lands outside the popup.
func (m model) clickMenu(x, line int) (tea.Model, tea.Cmd) {
	labels := menuLabels()
	inner := m.innerWidth()
	pw := popupWidth(labels, inner)
	left := 1 + inner - pw - edgePad // the frame border occupies column 0
	top := m.popupTop(len(labels) + frameLines)
	i := line - top - 1 // skip the popup's top border
	if x < left || x >= left+pw || i < 0 || i >= len(labels) {
		m.mode = modeList
		return m, nil
	}
	return m.runMenu(i)
}

// popupTop is the content line a popup of n lines starts on so that its
// bottom border sits on the status line, right above the footer.
func (m model) popupTop(n int) int {
	top := m.innerHeight() - 1 - n
	if top < 0 {
		return 0
	}
	return top
}

// popupWidth is the outer width of a popup around lines: the widest line
// plus padding and borders, capped so it fits inside the frame with the
// edge gap, so overlay can always draw it.
func popupWidth(lines []string, inner int) int {
	w := 0
	for _, l := range lines {
		w = max(w, ansi.StringWidth(l))
	}
	w += 2*popupPad + frameCells
	return max(min(w, inner-edgePad), frameCells+1)
}

// renderPopup draws a rounded box around lines, each padded to the widest
// entry, highlighting the line at index highlight (-1 for none). Every
// returned line is popupWidth cells wide.
func renderPopup(lines []string, highlight, inner int) []string {
	w := popupWidth(lines, inner) - frameCells
	hline := strings.Repeat("─", w)
	side := borderStyle.Render("│")
	out := make([]string, 0, len(lines)+frameLines)
	out = append(out, borderStyle.Render("╭"+hline+"╮"))
	for i, l := range lines {
		text := fitWidth(strings.Repeat(" ", popupPad)+truncate(l, w-2*popupPad), w)
		style := popupItemStyle
		if i == highlight {
			style = popupSelectedStyle
		}
		out = append(out, side+style.Render(text)+side)
	}
	out = append(out, borderStyle.Render("╰"+hline+"╯"))
	return out
}

// overlay draws popup over lines, right-aligned with an edgePad gap to the
// frame, starting at content line top. Lines keep their exact width.
func overlay(lines, popup []string, top, inner int) []string {
	out := make([]string, len(lines))
	copy(out, lines)
	if len(popup) == 0 {
		return out
	}
	pw := ansi.StringWidth(popup[0])
	left := inner - pw - edgePad
	if left < 0 {
		return out
	}
	for i, p := range popup {
		idx := top + i
		if idx < 0 || idx >= len(out) {
			continue
		}
		out[idx] = fitWidth(ansi.Truncate(out[idx], left, "")+p, inner)
	}
	return out
}

// confirmPopup is the y/n box for close and delete.
func (m model) confirmPopup(inner int) []string {
	verb := "close"
	if m.confirm == actionDelete {
		verb = "delete"
	}
	lines := []string{
		popupTitleStyle.Render(verb + " agent?"),
		m.target,
		popupHintStyle.Render("y confirm · n cancel"),
	}
	return renderPopup(lines, -1, inner)
}

// restoreChrome is the lines around the restore popup's names: the popup's
// borders, its title, and the footer below it.
const restoreChrome = frameLines + 2

// restorePopup lists the trashed sessions with the cursor on trashIdx,
// scrolled so the highlighted name is always inside the popup.
func (m model) restorePopup(inner int) []string {
	fit := max(m.innerHeight()-restoreChrome, 1)
	offset := 0
	if m.trashIdx >= fit {
		offset = m.trashIdx - fit + 1
	}
	end := min(len(m.trashed), offset+fit)
	lines := append([]string{popupHintStyle.Render("restore")}, m.trashed[offset:end]...)
	return renderPopup(lines, m.trashIdx-offset+1, inner)
}
