package sidebar

import (
	"sort"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/sahilm/fuzzy"
)

// tmpLabel is the picker's first entry: a fresh scratch directory.
const tmpLabel = "tmp"

// pickItem is one picker entry.
type pickItem struct {
	name string
	path string
	tmp  bool
}

// picker is the repo picker shown for a new agent: a fuzzy filter over the
// backend's repos with tmp always on offer.
type picker struct {
	input   textinput.Model
	items   []pickItem // tmp first, then repos sorted by name
	loading bool
	err     error
	cursor  int
}

func newPicker() picker {
	return picker{input: newInput("/ ")}
}

// reset clears the filter and marks the repo list as loading.
func (p *picker) reset() {
	p.input.SetValue("")
	p.items = []pickItem{{name: tmpLabel, tmp: true}}
	p.loading = true
	p.err = nil
	p.cursor = 0
}

// setRepos installs the scanned repos after tmp.
func (p *picker) setRepos(repos []Repo, err error) {
	p.loading = false
	p.err = err
	sorted := make([]Repo, len(repos))
	copy(sorted, repos)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	p.items = make([]pickItem, 0, len(sorted)+1)
	p.items = append(p.items, pickItem{name: tmpLabel, tmp: true})
	for _, r := range sorted {
		p.items = append(p.items, pickItem{name: r.Name, path: r.Path})
	}
	p.cursor = 0
}

// names returns the item names in order, the source for the fuzzy matcher.
func (p picker) names() []string {
	out := make([]string, len(p.items))
	for i, it := range p.items {
		out[i] = it.name
	}
	return out
}

// matches returns the items that pass the filter, best match first.
func (p picker) matches() []pickItem {
	pattern := p.input.Value()
	if pattern == "" {
		return p.items
	}
	ranked := fuzzy.Find(pattern, p.names())
	out := make([]pickItem, len(ranked))
	for i, r := range ranked {
		out[i] = p.items[r.Index]
	}
	return out
}

// selected returns the item under the cursor.
func (p picker) selected() (pickItem, bool) {
	items := p.matches()
	if p.cursor < 0 || p.cursor >= len(items) {
		return pickItem{}, false
	}
	return items[p.cursor], true
}

func (m model) updatePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		m.picker.input.Blur()
		return m, nil
	case "enter":
		return m.pickDir()
	case "down", "ctrl+n":
		m.picker.cursor = clamp(m.picker.cursor+1, len(m.picker.matches()))
		return m, nil
	case "up", "ctrl+p":
		m.picker.cursor = clamp(m.picker.cursor-1, len(m.picker.matches()))
		return m, nil
	}
	var cmd tea.Cmd
	m.picker.input, cmd = m.picker.input.Update(msg)
	m.picker.cursor = clamp(m.picker.cursor, len(m.picker.matches()))
	return m, cmd
}

// pickerLines renders the picker: header, filter, one line per repo, footer.
func (m model) pickerLines(inner int) []string {
	n := m.innerHeight()
	lines := make([]string, 0, n)
	lines = append(lines, spread(inner, headerStyle.Render("new agent"), sortLabelStyle.Render("repos")))
	lines = append(lines, fitWidth(" "+m.picker.input.View(), inner))
	body := n - listChrome
	lines = append(lines, m.pickerRows(inner, body)...)
	lines = append(lines, m.pickerStatus(inner))
	lines = append(lines, spread(inner, footerHintStyle.Render("esc back"), footerHintStyle.Render("enter open")))
	return lines
}

// pickerRows renders up to n one-line rows, scrolled so the cursor is visible.
func (m model) pickerRows(inner, n int) []string {
	items := m.picker.matches()
	offset := 0
	if n > 0 && m.picker.cursor >= n {
		offset = m.picker.cursor - n + 1
	}
	lines := make([]string, 0, max(n, 0))
	for i := offset; i < len(items) && len(lines) < n; i++ {
		lines = append(lines, m.pickerRow(items[i], i == m.picker.cursor, inner))
	}
	for len(lines) < n {
		lines = append(lines, blank(inner))
	}
	return lines
}

func (m model) pickerRow(it pickItem, selected bool, inner int) string {
	style := pickerNameStyle
	if it.tmp {
		style = pickerTmpStyle
	}
	if selected {
		style = pickerSelectedStyle
	}
	text := fitWidth(" "+truncate(it.name, inner-edgePad-1), inner)
	return style.Render(text)
}

func (m model) pickerStatus(inner int) string {
	switch {
	case m.picker.loading:
		return fitWidth(statusStyle.Render(" scanning repos"+ellipsis), inner)
	case m.picker.err != nil:
		return fitWidth(errorStyle.Render(" "+truncate(m.picker.err.Error(), inner-edgePad-1)), inner)
	default:
		return blank(inner)
	}
}

// nameLines renders the name prompt that follows the picker.
func (m model) nameLines(inner int) []string {
	n := m.innerHeight()
	lines := make([]string, 0, n)
	lines = append(lines, spread(inner, headerStyle.Render("new agent"), sortLabelStyle.Render("name")))
	lines = append(lines, blank(inner))
	dir := "scratch directory"
	if !m.pending.tmp {
		dir = ShortenHome(m.pending.dir, m.home)
	}
	dir = truncate(dir, inner-edgePad-1)
	lines = append(lines, fitWidth(titleStyle.Render(" "+dir), inner))
	lines = append(lines, fitWidth(" "+m.input.View(), inner))
	for len(lines) < n-2 {
		lines = append(lines, blank(inner))
	}
	lines = append(lines, m.statusLine(inner))
	lines = append(lines, spread(inner, footerHintStyle.Render("esc back"), footerHintStyle.Render("enter create")))
	return lines
}
