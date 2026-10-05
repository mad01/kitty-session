package sidebar

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCursorMovesAndEnterFocuses(t *testing.T) {
	fb := &fakeBackend{agents: mockupAgents()}
	m := newTestModel(t, fb, "")
	if got := m.cursorName(); got != "thismoon" {
		t.Fatalf("initial cursor on %q, want thismoon", got)
	}
	m, _ = press(t, m, "j", "j", "k", "down")
	if got := m.cursorName(); got != "code-search-local" {
		t.Fatalf("cursor on %q, want code-search-local", got)
	}
	m, _ = press(t, m, "k", "k", "k", "up", "up")
	if m.cursor != 0 {
		t.Fatalf("cursor went negative: %d", m.cursor)
	}
	m, _ = press(t, m, "j", "enter")
	wantCalls(t, fb, "focus:dropbrain-app")
	if m.mode != modeList {
		t.Fatalf("mode = %v after focus", m.mode)
	}
}

func TestDigitJumpsAndFocuses(t *testing.T) {
	fb := &fakeBackend{agents: mockupAgents()}
	m := newTestModel(t, fb, "")
	m, _ = press(t, m, "4")
	if got := m.cursorName(); got != "migraine-me" {
		t.Fatalf("cursor on %q, want migraine-me", got)
	}
	wantCalls(t, fb, "focus:migraine-me")
	m, _ = press(t, m, "9") // past the end: no-op
	wantCalls(t, fb, "focus:migraine-me")
	if m.cursor != 3 {
		t.Fatalf("cursor moved to %d on an out-of-range digit", m.cursor)
	}
}

func TestOwnTabKeysFocusAgentWindowAndNeverQuit(t *testing.T) {
	fb := &fakeBackend{agents: mockupAgents()}
	m := newTestModel(t, fb, "kitty-session")
	for _, k := range []string{"q", "l", "tab", "ctrl+c"} {
		var cmd tea.Cmd
		m, cmd = press(t, m, k)
		for _, msg := range drain(cmd) {
			if _, quit := msg.(tea.QuitMsg); quit {
				t.Fatalf("key %q produced tea.Quit", k)
			}
		}
	}
	wantCalls(t, fb, "focus-agent", "focus-agent", "focus-agent")
}

func TestFilterNarrowsAndEscClears(t *testing.T) {
	m := newTestModel(t, &fakeBackend{agents: mockupAgents()}, "")
	m, _ = press(t, m, "/", "m", "i")
	if m.mode != modeFilter {
		t.Fatalf("mode = %v, want filter", m.mode)
	}
	if got := names(m.visible()); len(got) != 1 || got[0] != "migraine-me" {
		t.Fatalf("visible = %v", got)
	}
	m, _ = press(t, m, "enter")
	if m.mode != modeList || m.filter.Value() != "mi" {
		t.Fatalf("enter should keep the filter: mode %v value %q", m.mode, m.filter.Value())
	}
	m, _ = press(t, m, "esc")
	if m.filter.Value() != "" || len(m.visible()) != 6 {
		t.Fatalf("esc should clear the filter: %q", m.filter.Value())
	}
}

func TestMenuOpensClosesAndDispatches(t *testing.T) {
	fb := &fakeBackend{agents: mockupAgents()}
	m := newTestModel(t, fb, "")
	m, _ = press(t, m, "m")
	if m.mode != modeMenu {
		t.Fatalf("m should open the menu, mode = %v", m.mode)
	}
	m, _ = press(t, m, "m")
	if m.mode != modeList {
		t.Fatalf("m again should close the menu, mode = %v", m.mode)
	}
	m, _ = press(t, m, "m", "esc")
	if m.mode != modeList {
		t.Fatalf("esc should close the menu, mode = %v", m.mode)
	}

	// shell split is the sixth entry, hooks status the seventh.
	m, _ = press(t, m, "m", "j", "j", "j", "j", "j", "enter")
	m, _ = press(t, m, "m", "down", "down", "down", "down", "down", "down", "enter")
	wantCalls(t, fb, "shell-split", "hooks")
	if m.status != "hooks ok" {
		t.Fatalf("status = %q, want the hooks answer", m.status)
	}

	// new agent is the first entry.
	m, _ = press(t, m, "m", "enter")
	if m.mode != modePicker {
		t.Fatalf("menu new agent should open the picker, mode = %v", m.mode)
	}
}

func TestMenuQuitAsksBackendThenQuits(t *testing.T) {
	fb := &fakeBackend{agents: mockupAgents()}
	m := newTestModel(t, fb, "")
	last := len(menuEntries) - 1
	keys := []string{"m"}
	for range last {
		keys = append(keys, "j")
	}
	m, cmd := press(t, m, append(keys, "enter")...)
	wantCalls(t, fb, "quit")
	msgs := drain(cmd)
	if len(msgs) != 1 {
		t.Fatalf("want one message, got %v", msgs)
	}
	if _, ok := msgs[0].(tea.QuitMsg); !ok {
		t.Fatalf("want tea.QuitMsg, got %T", msgs[0])
	}

	fb.fail = errFake
	m, cmd = press(t, m, append(keys, "enter")...)
	if cmd != nil || !m.statusErr {
		t.Fatalf("a failing Quit must not exit: cmd %v status %q", cmd, m.status)
	}
}

func TestRenameInline(t *testing.T) {
	fb := &fakeBackend{agents: mockupAgents()}
	m := newTestModel(t, fb, "")
	m, _ = press(t, m, "r")
	if m.mode != modeRename || m.input.Value() != "thismoon" {
		t.Fatalf("rename should prefill the name: mode %v value %q", m.mode, m.input.Value())
	}
	m, _ = press(t, m, "-2", "enter")
	wantCalls(t, fb, "rename:thismoon:thismoon-2")
	if m.mode != modeList {
		t.Fatalf("mode = %v after rename", m.mode)
	}
	m, _ = press(t, m, "r", "esc")
	wantCalls(t, fb, "rename:thismoon:thismoon-2")
	m, _ = press(t, m, "r", "enter") // unchanged name: no call
	wantCalls(t, fb, "rename:thismoon:thismoon-2")
}

func TestConfirmCloseAndDelete(t *testing.T) {
	fb := &fakeBackend{agents: mockupAgents()}
	m := newTestModel(t, fb, "")
	m, _ = press(t, m, "j", "c")
	if m.mode != modeConfirm || m.target != "dropbrain-app" {
		t.Fatalf("c should confirm on the cursor row: mode %v target %q", m.mode, m.target)
	}
	m, _ = press(t, m, "n")
	wantCalls(t, fb)
	m, _ = press(t, m, "c", "y")
	m, _ = press(t, m, "d", "enter")
	m, _ = press(t, m, "d", "esc")
	wantCalls(t, fb, "close:dropbrain-app:true", "close:dropbrain-app:false")
	if m.mode != modeList {
		t.Fatalf("mode = %v", m.mode)
	}
}

func TestRestorePicksTrashed(t *testing.T) {
	fb := &fakeBackend{agents: mockupAgents(), trashed: []string{"alpha", "beta"}}
	m := newTestModel(t, fb, "")
	m, _ = press(t, m, "u", "j", "enter")
	wantCalls(t, fb, "restore:beta")
	if m.mode != modeList {
		t.Fatalf("mode = %v", m.mode)
	}

	empty := &fakeBackend{agents: mockupAgents()}
	m = newTestModel(t, empty, "")
	m, _ = press(t, m, "u")
	if m.mode != modeList || m.status == "" {
		t.Fatalf("empty trash should stay in list mode with a status, got mode %v", m.mode)
	}
}

func TestPickerCreatesFromRepoAndTmp(t *testing.T) {
	fb := &fakeBackend{agents: mockupAgents(), repos: []Repo{
		{Name: "mad01/zeta", Path: "/r/zeta"},
		{Name: "mad01/kitty-session", Path: "/r/kitty-session"},
	}}
	m := newTestModel(t, fb, "")
	m, cmd := press(t, m, "n")
	if m.mode != modePicker || !m.picker.loading || cmd == nil {
		t.Fatalf("n should open a loading picker: mode %v loading %v", m.mode, m.picker.loading)
	}
	m = update(t, m, reposMsg{repos: fb.repos})
	if got := m.picker.items; len(got) != 3 || got[0].name != tmpLabel || got[1].name != "mad01/kitty-session" {
		t.Fatalf("picker items = %+v", got)
	}
	m, _ = press(t, m, "kitty", "enter")
	wantCalls(t, fb, "new:sug-kitty-session:/r/kitty-session")
	if m.mode != modeList {
		t.Fatalf("mode = %v after create", m.mode)
	}

	fb.calls = nil
	m, _ = press(t, m, "n")
	m = update(t, m, reposMsg{repos: fb.repos})
	m, _ = press(t, m, "enter") // tmp is first with an empty filter
	wantCalls(t, fb, "tmpdir", "new:sug-ks-fake:/tmp/ks-fake")
}

func TestPickerFallsBackToNamePromptOnError(t *testing.T) {
	fb := &fakeBackend{agents: mockupAgents(), repos: []Repo{{Name: "mad01/zeta", Path: "/r/zeta"}}}
	fb.fail = errFake
	m := newTestModel(t, fb, "")
	m, _ = press(t, m, "n")
	m = update(t, m, reposMsg{repos: fb.repos})
	m, _ = press(t, m, "zeta", "enter")
	if m.mode != modeName || m.input.Value() != "sug-zeta" || !m.statusErr {
		t.Fatalf("want name prompt with error: mode %v value %q status %q", m.mode, m.input.Value(), m.status)
	}
	fb.fail = nil
	fb.calls = nil
	m, _ = press(t, m, "2", "enter")
	wantCalls(t, fb, "new:sug-zeta2:/r/zeta")
	if m.mode != modeList {
		t.Fatalf("mode = %v", m.mode)
	}
}

func TestWindowSizePinsWidth(t *testing.T) {
	fb := &fakeBackend{}
	m := newModel(Options{Width: DefaultWidth, Backend: fb}, "/home/u")
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 42, Height: 20})
	drain(cmd)
	if fb.pinned != 42 {
		t.Fatalf("pinned = %d, want 42", fb.pinned)
	}
}

func TestMouseFooterAndRows(t *testing.T) {
	fb := &fakeBackend{agents: mockupAgents()}
	m := newTestModel(t, fb, "")
	footerY := testLines - 2
	click := func(x, y int) tea.MouseMsg {
		return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	}
	m = update(t, m, click(2, footerY))
	if m.mode != modePicker {
		t.Fatalf("footer left click should open the picker, mode = %v", m.mode)
	}
	m, _ = press(t, m, "esc")
	m = update(t, m, click(DefaultWidth-3, footerY))
	if m.mode != modeMenu {
		t.Fatalf("footer right click should open the menu, mode = %v", m.mode)
	}
	m = update(t, m, click(2, 1)) // outside the popup closes it
	if m.mode != modeList {
		t.Fatalf("click outside the menu should close it, mode = %v", m.mode)
	}
	m = update(t, m, click(5, 6)) // third content line pair -> second row
	wantCalls(t, fb, "focus:dropbrain-app")
}

func TestApplyAgentsKeepsCursorOnSameAgent(t *testing.T) {
	fb := &fakeBackend{agents: mockupAgents()}
	m := newTestModel(t, fb, "")
	m, _ = press(t, m, "j", "j") // code-search-local
	fb.agents[4].State = StateStopped // it drops to the bottom
	m = load(t, m)
	if got := m.cursorName(); got != "code-search-local" {
		t.Fatalf("cursor on %q, want code-search-local", got)
	}
	if m.cursor != 4 {
		t.Fatalf("cursor index = %d, want 4", m.cursor)
	}
}
