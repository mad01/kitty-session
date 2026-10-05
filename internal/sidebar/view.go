package sidebar

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// View renders the frame: every line is exactly frameWidth cells and the
// output is exactly frameHeight lines, so the sidebar fills its window.
func (m model) View() string {
	inner := m.innerWidth()
	var lines []string
	switch m.mode {
	case modePicker:
		lines = m.pickerLines(inner)
	case modeName:
		lines = m.nameLines(inner)
	default:
		lines = m.listLines(inner)
	}
	lines = fitLines(lines, m.innerHeight(), inner)

	hline := strings.Repeat("─", inner)
	side := borderStyle.Render("│")
	var b strings.Builder
	b.WriteString(borderStyle.Render("╭" + hline + "╮"))
	for _, l := range lines {
		b.WriteString("\n" + side + l + side)
	}
	b.WriteString("\n" + borderStyle.Render("╰"+hline+"╯"))
	return b.String()
}

// listLines renders the agent list with any popup the mode calls for.
func (m model) listLines(inner int) []string {
	n := m.innerHeight()
	lines := make([]string, 0, n)
	lines = append(
		lines,
		spread(inner, headerStyle.Render("agents"), sortLabelStyle.Render("priority")),
	)
	lines = append(lines, m.filterLine(inner))
	lines = append(lines, m.rowLines(inner, n-listChrome)...)
	lines = append(lines, m.statusLine(inner))
	lines = append(lines, spread(inner, footerStyle.Render("new"), footerStyle.Render("menu")))

	var popup []string
	switch m.mode {
	case modeMenu:
		popup = renderPopup(menuLabels(), m.menu.cursor, inner)
	case modeConfirm:
		popup = m.confirmPopup(inner)
	case modeRestore:
		popup = m.restorePopup(inner)
	default:
		return lines
	}
	return overlay(lines, popup, m.popupTop(len(popup)), inner)
}

// filterLine shows the `/` filter while it is being edited or has text.
func (m model) filterLine(inner int) string {
	if m.mode != modeFilter && m.filter.Value() == "" {
		return blank(inner)
	}
	return fitWidth(" "+filterPromptStyle.Render("/ ")+m.filter.View(), inner)
}

// statusLine shows the transient status or error above the footer.
func (m model) statusLine(inner int) string {
	if m.status == "" {
		return blank(inner)
	}
	style := statusStyle
	if m.statusErr {
		style = errorStyle
	}
	return fitWidth(style.Render(" "+truncate(m.status, inner-edgePad-1)), inner)
}

// rowLines renders the visible rows into exactly n lines.
func (m model) rowLines(inner, n int) []string {
	if n < 0 {
		n = 0
	}
	visible := m.visible()
	lines := make([]string, 0, n)
	for i := m.rowOffset(); i < len(visible) && len(lines)+rowHeight <= n; i++ {
		lines = append(lines, m.renderRow(visible[i], i == m.cursor, inner)...)
	}
	for len(lines) < n {
		lines = append(lines, blank(inner))
	}
	return lines
}

// renderRow draws one agent as two lines: dot and name, then the title.
func (m model) renderRow(a Agent, selected bool, inner int) []string {
	var bg lipgloss.TerminalColor = lipgloss.NoColor{}
	marker := " "
	if a.Own {
		bg = colorOwnBG
		marker = ownMarker
	}
	if selected {
		bg = colorPickBG
	}
	textWidth := inner - rowIndent - edgePad

	var head string
	if selected && m.mode == modeRename {
		head = ansi.Truncate(m.input.View(), textWidth, "")
	} else {
		head = seg(nameStyle, bg, truncate(a.Name, textWidth))
	}
	first := seg(markerStyle, bg, marker) + m.dot(a.State, bg) + seg(plainStyle, bg, " ") + head

	title := a.Title
	if title == "" {
		title = ShortenHome(a.Dir, m.home)
	}
	second := seg(titleStyle, bg, strings.Repeat(" ", rowIndent)+truncate(title, textWidth))

	return []string{padRow(first, inner, bg), padRow(second, inner, bg)}
}

// dot returns the styled state glyph; working and input pulse with the frame.
func (m model) dot(s State, bg lipgloss.TerminalColor) string {
	switch s {
	case StateInput:
		return seg(pulse(inputPulse, m.frame), bg, dotFilled)
	case StateDone:
		return seg(dotDone, bg, dotFilled)
	case StateWorking:
		return seg(pulse(workingPulse, m.frame), bg, dotFilled)
	case StateIdle:
		return seg(dotIdle, bg, dotHollow)
	default:
		return seg(dotStopped, bg, dotFaint)
	}
}

func pulse(cycle []lipgloss.AdaptiveColor, frame int) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(cycle[frame%len(cycle)])
}

// seg renders s in style over bg. Each segment carries its own background
// so a reset inside one segment cannot drop the row's background.
func seg(style lipgloss.Style, bg lipgloss.TerminalColor, s string) string {
	return style.Background(bg).Render(s)
}

// padRow extends a row line to inner cells with background-colored spaces.
func padRow(line string, inner int, bg lipgloss.TerminalColor) string {
	gap := inner - ansi.StringWidth(line)
	if gap > 0 {
		line += seg(plainStyle, bg, strings.Repeat(" ", gap))
	}
	return fitWidth(line, inner)
}

// spread places left at the left edge and right at the right edge of a line
// of inner cells, one cell in from each border. When both do not fit the
// right label is dropped.
func spread(inner int, left, right string) string {
	avail := inner - 2*edgePad
	gap := avail - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		return fitWidth(" "+truncate(left, avail), inner)
	}
	return fitWidth(" "+left+strings.Repeat(" ", gap)+right, inner)
}

// truncate cuts s to at most w cells, ending in an ellipsis when it was cut.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, ellipsis)
}

// fitWidth pads or hard-cuts s so it is exactly w cells wide. A wide
// character straddling the cut is dropped and the gap padded.
func fitWidth(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) > w {
		s = ansi.Truncate(s, w, "")
	}
	if gap := w - ansi.StringWidth(s); gap > 0 {
		s += strings.Repeat(" ", gap)
	}
	return s
}

// fitLines pads or cuts lines to exactly n entries of exactly w cells.
func fitLines(lines []string, n, w int) []string {
	if n < 0 {
		n = 0
	}
	out := make([]string, n)
	for i := range out {
		if i < len(lines) {
			out[i] = fitWidth(lines[i], w)
		} else {
			out[i] = blank(w)
		}
	}
	return out
}

func blank(w int) string {
	if w <= 0 {
		return ""
	}
	return strings.Repeat(" ", w)
}
