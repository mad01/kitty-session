package tui

import (
	"fmt"
	"io"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

type delegateKeyMap struct {
	open   key.Binding
	newSes key.Binding
	close  key.Binding
	delete key.Binding
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
	}
}

func (d delegateKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{d.open, d.newSes, d.close, d.delete}
}

func (d delegateKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{d.open, d.newSes, d.close, d.delete},
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

func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(sessionItem)
	if !ok {
		return
	}

	isSelected := index == m.Index()

	name := fmt.Sprintf("%-16s", item.Title())

	var badge string
	if isSelected {
		if item.running {
			badge = selectedRunningBadge.String()
		} else {
			badge = selectedStoppedBadge.String()
		}
	} else {
		if item.running {
			badge = runningBadge.String()
		} else {
			badge = stoppedBadge.String()
		}
	}

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
