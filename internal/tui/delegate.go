package tui

import (
	"fmt"
	"io"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mad01/kitty-session/internal/claude"
)

// animFrame is the current animation frame for pulsing badges.
// Updated by the model's animTickMsg handler; read by stateBadge.
var animFrame int

// Indigo pulse cycle: bright → dim → bright
var workingPulseColors = []lipgloss.Color{
	"#7571F9", // base indigo
	"#615DBF", // medium
	"#4D4A96", // dim
	"#615DBF", // medium
}

// Amber pulse cycle: bright → dim → bright
var inputPulseColors = []lipgloss.Color{
	"#FFBF00", // base amber
	"#CC9900", // medium
	"#997300", // dim
	"#CC9900", // medium
}

type delegateKeyMap struct {
	open   key.Binding
	newSes key.Binding
	close  key.Binding
	delete key.Binding
	rename key.Binding
}

func newDelegateKeyMap() *delegateKeyMap {
	return &delegateKeyMap{
		open: key.NewBinding(
			key.WithKeys("enter", "o"),
			key.WithHelp("o/enter", "open"),
		),
		newSes: key.NewBinding(
			key.WithKeys("n"),
			key.WithHelp("n", "new"),
		),
		close: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "close tab"),
		),
		delete: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "delete"),
		),
		rename: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "rename"),
		),
	}
}

func (d delegateKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{d.open, d.newSes, d.close, d.delete, d.rename}
}

func (d delegateKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{d.open, d.newSes, d.close, d.delete, d.rename},
	}
}

// Compile-time interface check.
var _ list.ItemDelegate = itemDelegate{}

type itemDelegate struct {
	keys *delegateKeyMap
}

func newItemDelegate() itemDelegate {
	return itemDelegate{keys: newDelegateKeyMap()}
}

func (d itemDelegate) Height() int                             { return 2 }
func (d itemDelegate) Spacing() int                            { return 0 }
func (d itemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

// stateBadge returns the rendered badge string for a given state.
func stateBadge(state claude.State, selected bool) string {
	switch state {
	case claude.StateWorking:
		pulseColor := workingPulseColors[animFrame%len(workingPulseColors)]
		icon := lipgloss.NewStyle().Foreground(pulseColor).Render("●")
		text := lipgloss.NewStyle().Foreground(colorAccent)
		if selected {
			text = text.Bold(true)
		}
		return icon + text.Render(" working")
	case claude.StateNeedsInput:
		pulseColor := inputPulseColors[animFrame%len(inputPulseColors)]
		icon := lipgloss.NewStyle().Foreground(pulseColor).Render("◆")
		text := lipgloss.NewStyle().Foreground(colorAmber)
		if selected {
			text = text.Bold(true)
		}
		return icon + text.Render(" input")
	case claude.StateIdle:
		if selected {
			return selectedIdleBadge.String()
		}
		return idleBadge.String()
	case claude.StateWaiting:
		if selected {
			return selectedWaitingBadge.String()
		}
		return waitingBadge.String()
	default:
		if selected {
			return selectedStoppedBadge.String()
		}
		return stoppedBadge.String()
	}
}

func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(sessionItem)
	if !ok {
		return
	}

	isSelected := index == m.Index()

	name := fmt.Sprintf("%-16s", item.Title())

	badge := stateBadge(item.state, isSelected)

	dir := item.Description()

	var row string
	if isSelected {
		cursor := cursorStyle.Render("▸ ")
		row = cursor + selectedNameStyle.Render(name) + " " + badge + "  " + selectedDirStyle.Render(dir)
	} else {
		row = "  " + normalStyle.Render(name) + " " + badge + "  " + dirStyle.Render(dir)
	}

	ctxLine := ""
	if item.context != "" {
		ctxLine = contextStyle.Render("    ╰ " + item.context)
	}

	fmt.Fprint(w, row+"\n"+ctxLine)
}
