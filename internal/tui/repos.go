package tui

import (
	"fmt"
	"io"
	"sort"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mad01/kitty-session/internal/repo/config"
	"github.com/mad01/kitty-session/internal/repo/finder"
)

// Compile-time interface check.
var _ list.Item = repoItem{}

// repoItem implements list.Item for the repo picker.
type repoItem struct {
	name  string // org/repo
	path  string // absolute filesystem path
	isTmp bool   // scratch temp directory
}

func newTmpRepoItem() repoItem {
	return repoItem{name: "tmp", isTmp: true}
}

func (r repoItem) Title() string { return r.name }
func (r repoItem) Description() string {
	if r.isTmp {
		return "new scratch directory"
	}
	return shortenDir(r.path)
}
func (r repoItem) FilterValue() string { return r.name }

// Compile-time interface check.
var _ list.ItemDelegate = repoDelegate{}

type repoDelegate struct{}

func (d repoDelegate) Height() int                             { return 1 }
func (d repoDelegate) Spacing() int                            { return 0 }
func (d repoDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d repoDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(repoItem)
	if !ok {
		return
	}

	isSelected := index == m.Index()
	maxW := m.Width()

	name := item.name
	dir := item.Description()

	// Layout: prefix(2) + name + gap(2) + dir
	const prefix = 2 // "▸ " or "  "
	const gap = 2    // "  "

	avail := maxW - prefix - len(name) - gap
	if avail < 0 {
		// Name itself is too wide, truncate it and drop dir
		name = truncateStr(name, maxW-prefix)
		dir = ""
	} else {
		dir = truncateStr(dir, avail)
	}

	if isSelected {
		cursor := cursorStyle.Render("▸ ")
		var styledName string
		if item.isTmp {
			styledName = tmpSelectedStyle.Render(name)
		} else {
			styledName = selectedNameStyle.Render(name)
		}
		if dir != "" {
			fmt.Fprint(w, cursor+styledName+"  "+selectedDirStyle.Render(dir))
		} else {
			fmt.Fprint(w, cursor+styledName)
		}
	} else {
		var styledName string
		if item.isTmp {
			styledName = tmpNormalStyle.Render(name)
		} else {
			styledName = normalStyle.Render(name)
		}
		if dir != "" {
			fmt.Fprint(w, "  "+styledName+"  "+dirStyle.Render(dir))
		} else {
			fmt.Fprint(w, "  "+styledName)
		}
	}
}

// truncateStr cuts s to maxW characters, appending "..." if truncated.
func truncateStr(s string, maxW int) string {
	if len(s) <= maxW {
		return s
	}
	if maxW <= 3 {
		if maxW > 0 {
			return s[:maxW]
		}
		return ""
	}
	return s[:maxW-3] + "..."
}

// loadRepos scans configured directories for git repos and prepends a tmp item.
func loadRepos() []repoItem {
	var items []repoItem

	cfg, err := config.Load()
	if err == nil {
		repos, walkErr := finder.Walk(cfg.Dirs)
		if walkErr == nil {
			items = make([]repoItem, len(repos))
			for i, r := range repos {
				items[i] = repoItem{name: r.Name, path: r.Path}
			}
			sort.Slice(items, func(i, j int) bool {
				return items[i].name < items[j].name
			})
		}
	}

	// Always prepend tmp so the picker is never empty.
	return append([]repoItem{newTmpRepoItem()}, items...)
}
