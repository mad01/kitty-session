package sidebar

import (
	"errors"
	"os"
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
	m, cmd := press(t, m, "j", "enter")
	wantCalls(t, fb) // nothing until the command runs
	m, refresh := feed(t, m, cmd)
	wantCalls(t, fb, "focus:dropbrain-app")
	if m.mode != modeList || refresh == nil {
		t.Fatalf("after focus: mode %v, refresh cmd %v", m.mode, refresh)
	}
}

func TestDigitJumpsAndFocuses(t *testing.T) {
	fb := &fakeBackend{agents: mockupAgents()}
	m := newTestModel(t, fb, "")
	m, _ = run(t, m, "4")
	if got := m.cursorName(); got != "migraine-me" {
		t.Fatalf("cursor on %q, want migraine-me", got)
	}
	wantCalls(t, fb, "focus:migraine-me")
	m, _ = run(t, m, "9") // past the end: no-op
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
		m, cmd = run(t, m, k)
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
	m, _ = run(t, m, "m", "j", "j", "j", "j", "j", "enter")
	m, _ = run(t, m, "m", "down", "down", "down", "down", "down", "down", "enter")
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
	keys := []string{"m"}
	for range len(menuEntries) - 1 {
		keys = append(keys, "j")
	}
	keys = append(keys, "enter")

	m, cmd := run(t, m, keys...)
	wantCalls(t, fb, "quit")
	msgs := drain(cmd)
	if len(msgs) != 1 {
		t.Fatalf("want one message after Quit succeeded, got %v", msgs)
	}
	if _, ok := msgs[0].(tea.QuitMsg); !ok {
		t.Fatalf("want tea.QuitMsg, got %T", msgs[0])
	}

	fb.fail = errFake
	m, cmd = run(t, m, keys...)
	if cmd != nil || !m.statusErr {
		t.Fatalf("a failing Quit must not exit: cmd %v status %q", cmd, m.status)
	}
}

func TestMenuClickNeedsPopupColumns(t *testing.T) {
	fb := &fakeBackend{agents: mockupAgents()}
	m := newTestModel(t, fb, "")
	inner := m.innerWidth()
	pw := popupWidth(menuLabels(), inner)
	left := 1 + inner - pw - edgePad
	top := m.popupTop(len(menuEntries) + frameLines)
	shellSplitY := top + 1 + 5 + 1 // popup border, sixth entry, frame top border

	m, _ = press(t, m, "m")
	m = update(t, m, click(left-3, shellSplitY)) // same row, left of the popup
	if m.mode != modeList {
		t.Fatalf("click outside the popup columns should close the menu, mode = %v", m.mode)
	}
	wantCalls(t, fb)

	m, _ = press(t, m, "m")
	next, cmd := m.Update(click(left+2, shellSplitY))
	m, _ = feed(t, asModel(t, next), cmd)
	wantCalls(t, fb, "shell-split")
	if m.mode != modeList {
		t.Fatalf("mode = %v after a menu click", m.mode)
	}
}

func TestRenameInline(t *testing.T) {
	fb := &fakeBackend{agents: mockupAgents()}
	m := newTestModel(t, fb, "")
	m, _ = press(t, m, "r")
	if m.mode != modeRename || m.input.Value() != "thismoon" {
		t.Fatalf("rename should prefill the name: mode %v value %q", m.mode, m.input.Value())
	}
	m, _ = run(t, m, "-2", "enter")
	wantCalls(t, fb, "rename:thismoon:thismoon-2")
	if m.mode != modeList || m.follow != "thismoon-2" {
		t.Fatalf("after rename: mode %v follow %q", m.mode, m.follow)
	}
	m, _ = run(t, m, "r", "esc")
	m, _ = run(t, m, "r", "enter") // unchanged name: no call
	wantCalls(t, fb, "rename:thismoon:thismoon-2")
}

func TestConfirmCloseAndDelete(t *testing.T) {
	fb := &fakeBackend{agents: mockupAgents()}
	m := newTestModel(t, fb, "")
	m, _ = press(t, m, "j", "c")
	if m.mode != modeConfirm || m.target != "dropbrain-app" {
		t.Fatalf("c should confirm on the cursor row: mode %v target %q", m.mode, m.target)
	}
	m, _ = run(t, m, "n")
	wantCalls(t, fb)
	m, _ = run(t, m, "c", "y")
	m, _ = run(t, m, "d", "enter")
	m, _ = run(t, m, "d", "esc")
	wantCalls(t, fb, "close:dropbrain-app:true", "close:dropbrain-app:false")
	if m.mode != modeList {
		t.Fatalf("mode = %v", m.mode)
	}
}

func TestRestorePicksTrashed(t *testing.T) {
	fb := &fakeBackend{agents: mockupAgents(), trashed: []string{"alpha", "beta"}}
	m := newTestModel(t, fb, "")
	m, _ = run(t, m, "u", "j", "enter")
	wantCalls(t, fb, "restore:beta")
	if m.mode != modeList || m.follow != "beta" {
		t.Fatalf("after restore: mode %v follow %q", m.mode, m.follow)
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
	m, _ = run(t, m, "kitty", "enter")
	wantCalls(t, fb, "new:sug-kitty-session:/r/kitty-session")
	if m.mode != modeList || m.status != "created sug-kitty-session" {
		t.Fatalf("after create: mode %v status %q", m.mode, m.status)
	}

	fb.calls = nil
	m, _ = press(t, m, "n")
	m = update(t, m, reposMsg{repos: fb.repos})
	m, cmd = press(t, m, "enter") // tmp is first with an empty filter
	wantCalls(t, fb)              // TmpDir is deferred to the command
	m, _ = feed(t, m, cmd)
	wantCalls(t, fb, "tmpdir", "new:tmp-1005-1200:/tmp/ks-fake")
	if m.follow != "tmp-1005-1200" {
		t.Fatalf("follow = %q", m.follow)
	}
}

func TestPickerFallsBackToNamePromptOnError(t *testing.T) {
	fb := &fakeBackend{agents: mockupAgents(), repos: []Repo{{Name: "mad01/zeta", Path: "/r/zeta"}}}
	fb.fail = errFake
	m := newTestModel(t, fb, "")
	m, _ = press(t, m, "n")
	m = update(t, m, reposMsg{repos: fb.repos})
	m, _ = run(t, m, "zeta", "enter")
	if m.mode != modeName || m.input.Value() != "sug-zeta" || !m.statusErr {
		t.Fatalf("want name prompt with error: mode %v value %q status %q", m.mode, m.input.Value(), m.status)
	}
	fb.fail = nil
	fb.calls = nil
	m, _ = run(t, m, "2", "enter")
	wantCalls(t, fb, "new:sug-zeta2:/r/zeta")
	if m.mode != modeList {
		t.Fatalf("mode = %v", m.mode)
	}
}

func TestNamePromptRejectsEmptyName(t *testing.T) {
	fb := &fakeBackend{agents: mockupAgents(), noSuggest: true,
		repos: []Repo{{Name: "mad01/zeta", Path: "/r/zeta"}}}
	m := newTestModel(t, fb, "")
	m, _ = press(t, m, "n")
	m = update(t, m, reposMsg{repos: fb.repos})
	m, _ = press(t, m, "zeta", "enter")
	if m.mode != modeName || m.input.Value() != "" {
		t.Fatalf("no suggestion should open an empty prompt: mode %v value %q", m.mode, m.input.Value())
	}
	m, cmd := press(t, m, "   ", "enter")
	if cmd != nil || m.mode != modeName || m.status != errNameRequired.Error() || !m.statusErr {
		t.Fatalf("empty name accepted: cmd %v mode %v status %q", cmd, m.mode, m.status)
	}
	wantCalls(t, fb)
	m, _ = run(t, m, "ok", "enter")
	wantCalls(t, fb, "new:ok:/r/zeta")
}

func TestTmpDirCreatedBeforeNewAndRemovedOnFailure(t *testing.T) {
	base := t.TempDir()
	fb := &fakeBackend{agents: mockupAgents(), tmpBase: base, fail: errFake}
	m := newTestModel(t, fb, "")
	m, _ = press(t, m, "n")
	m = update(t, m, reposMsg{})
	m, _ = run(t, m, "enter")
	if len(fb.calls) != 2 || fb.calls[0] != "tmpdir" {
		t.Fatalf("calls = %v, want tmpdir then new", fb.calls)
	}
	if entries, _ := os.ReadDir(base); len(entries) != 0 {
		t.Fatalf("scratch dir left behind after New failed: %v", entries)
	}
	if m.mode != modeName || !m.pending.tmp || m.input.Value() != "tmp-1005-1200" {
		t.Fatalf("want name prompt for the tmp attempt: mode %v pending %+v", m.mode, m.pending)
	}

	fb.fail = nil
	fb.calls = nil
	m, _ = run(t, m, "enter")
	entries, _ := os.ReadDir(base)
	if len(entries) != 1 || len(fb.calls) != 2 || fb.calls[0] != "tmpdir" {
		t.Fatalf("want one scratch dir kept and tmpdir before new: %v %v", entries, fb.calls)
	}
	if m.mode != modeList {
		t.Fatalf("mode = %v", m.mode)
	}
}

func TestStaleListResultIsDropped(t *testing.T) {
	fb := &fakeBackend{agents: mockupAgents()}
	m := newTestModel(t, fb, "")
	older := m.listCmd()
	fb.agents = mockupAgents()[:2]
	newer := m.listCmd()
	oldMsg := older()
	newMsg := newer()

	// Newer result first, then the stale one: the stale one must not win.
	m = update(t, m, newMsg)
	m = update(t, m, oldMsg)
	if len(m.agents) != 2 {
		t.Fatalf("stale result applied: %d agents", len(m.agents))
	}

	// Stale result arriving before the newer one is dropped as well.
	m2 := newTestModel(t, fb, "")
	fb.agents = mockupAgents()
	older = m2.listCmd()
	oldMsg = older()
	fb.agents = mockupAgents()[:3]
	newer = m2.listCmd()
	m2 = update(t, m2, oldMsg)
	if len(m2.agents) != 2 {
		t.Fatalf("stale result applied ahead of the newer List: %d agents", len(m2.agents))
	}
	m2 = update(t, m2, newer())
	if len(m2.agents) != 3 {
		t.Fatalf("latest result not applied: %d agents", len(m2.agents))
	}
}

func TestWindowSizePinsWidthAndReportsErrors(t *testing.T) {
	fb := &fakeBackend{}
	m := newModel(Options{Width: DefaultWidth, Backend: fb}, "/home/u")
	next, cmd := m.Update(tea.WindowSizeMsg{Width: 42, Height: 20})
	m, _ = feed(t, asModel(t, next), cmd)
	if fb.pinned != 42 || m.statusErr {
		t.Fatalf("pinned = %d, statusErr %v", fb.pinned, m.statusErr)
	}

	fb.pinErr = errors.New("pin failed\nsecond line")
	next, cmd = m.Update(tea.WindowSizeMsg{Width: 50, Height: 20})
	m, _ = feed(t, asModel(t, next), cmd)
	if !m.statusErr || m.status != "pin failed" {
		t.Fatalf("PinWidth error not shown: statusErr %v status %q", m.statusErr, m.status)
	}
}

func TestMouseFooterAndRows(t *testing.T) {
	fb := &fakeBackend{agents: mockupAgents()}
	m := newTestModel(t, fb, "")
	footerY := testLines - 2
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
	next, cmd := m.Update(click(5, 6)) // third content line pair -> second row
	m, _ = feed(t, asModel(t, next), cmd)
	wantCalls(t, fb, "focus:dropbrain-app")
	if m.cursor != 1 {
		t.Fatalf("cursor = %d after a row click", m.cursor)
	}
}

func TestApplyAgentsKeepsCursorOnSameAgent(t *testing.T) {
	fb := &fakeBackend{agents: mockupAgents()}
	m := newTestModel(t, fb, "")
	m, _ = press(t, m, "j", "j")        // code-search-local
	fb.agents[4].State = StateStopped   // it drops to the bottom
	m = load(t, m)
	if got := m.cursorName(); got != "code-search-local" {
		t.Fatalf("cursor on %q, want code-search-local", got)
	}
	if m.cursor != 4 {
		t.Fatalf("cursor index = %d, want 4", m.cursor)
	}
}

func TestFirstLine(t *testing.T) {
	tests := map[string]string{
		"one":                    "one",
		"  padded  ":             "padded",
		"first\nsecond\nthird":   "first",
		"\nleading newline":      "",
		"trailing newline\n":     "trailing newline",
		"":                       "",
	}
	for in, want := range tests {
		if got := firstLine(in); got != want {
			t.Errorf("firstLine(%q) = %q, want %q", in, got, want)
		}
	}
}
