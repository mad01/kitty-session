package sidebar

import (
	"errors"
	"fmt"
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

// contains reports whether any line holds s.
func contains(ls []string, s string) bool {
	for _, l := range ls {
		if strings.Contains(l, s) {
			return true
		}
	}
	return false
}

func TestViewGeometryAcrossModes(t *testing.T) {
	fb := &fakeBackend{agents: mockupAgents(), trashed: []string{"old", "older"}}
	fb.repos = []Repo{{Name: "mad01/a-very-long-repository-name-that-overflows", Path: "/r/a"}}
	entries := []struct {
		name  string
		keys  []string
		async bool // run the last key's command: the mode is reached through a result
	}{
		{"list", nil, false},
		{"filter", []string{"/", "k"}, false},
		{"menu", []string{"m"}, false},
		{"confirm", []string{"d"}, false},
		{"restore", []string{"u"}, false},
		{"rename", []string{"r"}, false},
		{"picker", []string{"n"}, false},
		{"name", []string{"n", "enter"}, true},
	}
	for _, width := range []int{12, 20, 36, 60} {
		for _, height := range []int{6, 12, 24} {
			for _, e := range entries {
				t.Run(fmt.Sprintf("%s-w%d-h%d", e.name, width, height), func(t *testing.T) {
					fb.fail = errFake // force the name prompt after the picker
					m := newSizedModel(t, fb, "kitty-session", width, height)
					m = update(t, m, reposMsg{repos: fb.repos})
					m, cmd := press(t, m, e.keys...)
					if e.async {
						m, _ = feed(t, m, cmd)
					}
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

func TestOwnRowComesFromBackendOnly(t *testing.T) {
	fb := &fakeBackend{agents: mockupAgents()}
	m := newTestModel(t, fb, "thismoon") // Options.Session must not move the marker
	got := lines(m)
	if !strings.HasPrefix(got[11], "│▌○ kitty-session") {
		t.Errorf("own marker missing on the Own agent: %q", got[11])
	}
	if !strings.HasPrefix(got[3], "│ ● thismoon") {
		t.Errorf("Options.Session must not mark a row: %q", got[3])
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
	if !strings.HasPrefix(got[first], "│   Claude Code") {
		t.Errorf("row left of the popup lost: %q", got[first])
	}
}

func TestViewMenuAtWidth20(t *testing.T) {
	m := newSizedModel(t, &fakeBackend{agents: mockupAgents()}, "", 20, testLines)
	m, _ = press(t, m, "m")
	got := lines(m)
	assertGeometry(t, m, 20, testLines)
	for _, e := range menuEntries {
		if !contains(got, e.label) {
			t.Errorf("menu entry %q not drawn at width 20", e.label)
		}
	}
}

func TestViewConfirmWithLongName(t *testing.T) {
	long := strings.Repeat("abcdefgh", 4) // 32 cells, wider than the popup can be
	fb := &fakeBackend{agents: []Agent{{Name: long, State: StateIdle}}}
	m := newTestModel(t, fb, "")
	m, _ = press(t, m, "d")
	got := lines(m)
	assertGeometry(t, m, DefaultWidth, testLines)
	if !contains(got, "delete agent?") || !contains(got, "y confirm") {
		t.Errorf("confirm popup not drawn:\n%s", strings.Join(got, "\n"))
	}
	if !contains(got, long[:20]+"…") {
		t.Errorf("long name not truncated inside the popup:\n%s", strings.Join(got, "\n"))
	}
}

func TestViewRestoreScrollsToCursor(t *testing.T) {
	trashed := make([]string, 30)
	for i := range trashed {
		trashed[i] = fmt.Sprintf("trashed-%02d", i)
	}
	m := newTestModel(t, &fakeBackend{agents: mockupAgents(), trashed: trashed}, "")
	keys := []string{"u"}
	for range 25 {
		keys = append(keys, "j")
	}
	m, _ = press(t, m, keys...)
	got := lines(m)
	assertGeometry(t, m, DefaultWidth, testLines)
	if !contains(got, "trashed-25") {
		t.Errorf("highlighted trashed entry scrolled out of view:\n%s", strings.Join(got, "\n"))
	}
	if contains(got, "trashed-00") {
		t.Errorf("first entry should have scrolled away")
	}
	m, _ = run(t, m, "enter")
	if m.follow != "trashed-25" {
		t.Errorf("restored %q, want trashed-25", m.follow)
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
	if !strings.HasPrefix(got[3], "│▌○ kitty-session") || strings.Contains(got[5], "●") {
		t.Errorf("filter did not narrow rows: %q / %q", got[3], got[5])
	}
	m, _ = run(t, m, "enter") // Focus fails -> error on the status line
	got = lines(m)
	if !strings.Contains(got[testLines-3], errFake.Error()) {
		t.Errorf("status line = %q", got[testLines-3])
	}
}

func TestViewLongFilterKeepsWidth(t *testing.T) {
	m := newTestModel(t, &fakeBackend{agents: mockupAgents()}, "")
	m, _ = press(t, m, "/", strings.Repeat("x", 30))
	assertGeometry(t, m, DefaultWidth, testLines)
	m, _ = press(t, m, strings.Repeat("y", 20))
	assertGeometry(t, m, DefaultWidth, testLines)
}

func TestViewMultiLineErrorKeepsHeight(t *testing.T) {
	fb := &fakeBackend{agents: mockupAgents()}
	fb.fail = errors.New("first line of a long error\nsecond line\nthird line")
	m := newTestModel(t, fb, "")
	m, _ = run(t, m, "enter")
	assertGeometry(t, m, DefaultWidth, testLines)
	if m.status != "first line of a long error" || !m.statusErr {
		t.Errorf("status = %q", m.status)
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
