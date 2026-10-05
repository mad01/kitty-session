package sidebar

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// lines splits the view into stripped lines.
func lines(m model) []string {
	out := strings.Split(m.View(), "\n")
	for i, l := range out {
		out[i] = ansi.Strip(l)
	}
	return out
}

// assertGeometry checks every line is exactly width cells and there are height lines.
func assertGeometry(t *testing.T, m model, width, height int) {
	t.Helper()
	view := strings.Split(m.View(), "\n")
	if len(view) != height {
		t.Fatalf("got %d lines, want %d", len(view), height)
	}
	for i, l := range view {
		if w := ansi.StringWidth(l); w != width {
			t.Errorf("line %d is %d cells, want %d: %q", i, w, width, ansi.Strip(l))
		}
	}
}

func TestViewGeometryAcrossModes(t *testing.T) {
	fb := &fakeBackend{agents: mockupAgents(), trashed: []string{"old", "older"}}
	fb.repos = []Repo{{Name: "mad01/a-very-long-repository-name-that-overflows", Path: "/r/a"}}
	entries := []struct {
		name string
		keys []string
	}{
		{"list", nil},
		{"filter", []string{"/", "k"}},
		{"menu", []string{"m"}},
		{"confirm", []string{"d"}},
		{"restore", []string{"u"}},
		{"rename", []string{"r"}},
		{"picker", []string{"n"}},
		{"name", []string{"n", "enter"}},
	}
	for _, width := range []int{12, 20, 36, 60} {
		for _, height := range []int{6, 12, 24} {
			for _, e := range entries {
				t.Run(e.name, func(t *testing.T) {
					fb.fail = errFake // force the name prompt after the picker
					m := newModel(
						Options{Width: width, Backend: fb, Session: "kitty-session"},
						"/home/u",
					)
					m = update(t, m, tea.WindowSizeMsg{Width: testCols, Height: height})
					m = load(t, m)
					m, _ = press(t, m, e.keys...)
					m = update(t, m, reposMsg{repos: fb.repos})
					assertGeometry(t, m, width, height)
				})
			}
		}
	}
}

func TestViewNarrowTerminalClampsWidth(t *testing.T) {
	fb := &fakeBackend{agents: mockupAgents()}
	m := newModel(Options{Width: 36, Backend: fb}, "/home/u")
	m = update(t, m, tea.WindowSizeMsg{Width: 20, Height: 10})
	m = load(t, m)
	assertGeometry(t, m, 20, 10)
}

func TestViewMockup(t *testing.T) {
	fb := &fakeBackend{agents: mockupAgents()}
	m := newTestModel(t, fb, "kitty-session")
	got := lines(m)

	want := map[int]string{
		0:  "╭──────────────────────────────────╮",
		1:  "│ agents                  priority │",
		2:  "│                                  │",
		3:  "│ ● thismoon                       │",
		4:  "│   Mods overview and integration… │",
		5:  "│ ● dropbrain-app                  │",
		6:  "│   Migrane iOS 27.1 migration ✳ … │",
		7:  "│ ● code-search-local              │",
		8:  "│   Reindex 日本語 テスト after s… │",
		9:  "│ ○ migraine-me                    │",
		10: "│   Migrane iOS 27.1 update        │",
		11: "│▌○ kitty-session                  │",
		12: "│   Claude Code                    │",
		13: "│ · dotfiles                       │",
		14: "│   ~/code/dotfiles                │",
		22: "│ new                         menu │",
		23: "╰──────────────────────────────────╯",
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("line %d\n got %q\nwant %q", i, got[i], w)
		}
	}
}

func TestViewEmptyList(t *testing.T) {
	m := newTestModel(t, &fakeBackend{}, "")
	got := lines(m)
	if !strings.Contains(got[1], "agents") || !strings.Contains(got[1], "priority") {
		t.Errorf("header missing: %q", got[1])
	}
	if !strings.Contains(got[testLines-2], "new") || !strings.Contains(got[testLines-2], "menu") {
		t.Errorf("footer missing: %q", got[testLines-2])
	}
	for i := 2; i < testLines-2; i++ {
		if strings.TrimSpace(strings.Trim(got[i], "│")) != "" {
			t.Errorf("body line %d not blank: %q", i, got[i])
		}
	}
}

func TestViewMenuPopup(t *testing.T) {
	m := newTestModel(t, &fakeBackend{agents: mockupAgents()}, "")
	m, _ = press(t, m, "m", "j")
	got := lines(m)
	footer := testLines - 2
	if !strings.HasSuffix(got[footer-1], "╰──────────────╯ │") {
		t.Errorf("popup bottom border not above footer: %q", got[footer-1])
	}
	first := footer - 1 - len(menuEntries)
	for i, e := range menuEntries {
		if !strings.Contains(got[first+i], e.label) {
			t.Errorf("menu line %d = %q, want %q", first+i, got[first+i], e.label)
		}
	}
	if !strings.HasPrefix(got[first], "│ · dotfiles") {
		t.Errorf("row left of the popup lost: %q", got[first])
	}
}

func TestViewFilterAndStatus(t *testing.T) {
	fb := &fakeBackend{agents: mockupAgents(), fail: errFake}
	m := newTestModel(t, fb, "")
	m, _ = press(t, m, "/", "kit", "enter")
	got := lines(m)
	if !strings.Contains(got[2], "/ kit") {
		t.Errorf("filter line = %q", got[2])
	}
	if !strings.HasPrefix(got[3], "│ ○ kitty-session") || strings.Contains(got[5], "●") {
		t.Errorf("filter did not narrow rows: %q / %q", got[3], got[5])
	}
	m, _ = press(t, m, "enter") // Focus fails -> error on the status line
	got = lines(m)
	if !strings.Contains(got[testLines-3], errFake.Error()) {
		t.Errorf("status line = %q", got[testLines-3])
	}
}

func TestTruncateAndFit(t *testing.T) {
	tests := []struct {
		s    string
		w    int
		want string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"a longer sentence", 8, "a longe…"},
		{"日本語テスト", 7, "日本語…"},
		{"anything", 0, ""},
	}
	for _, tt := range tests {
		if got := truncate(tt.s, tt.w); got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.w, got, tt.want)
		}
		if w := ansi.StringWidth(fitWidth(tt.s, tt.w)); tt.w > 0 && w != tt.w {
			t.Errorf("fitWidth(%q, %d) is %d cells", tt.s, tt.w, w)
		}
	}
}
