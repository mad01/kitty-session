package sidebar

import (
	"strings"
	"testing"
)

func TestKeysPopupOpensFromQuestionMarkAndMenu(t *testing.T) {
	fb := &fakeBackend{agents: mockupAgents()}
	m := newTestModel(t, fb, "")
	m, _ = press(t, m, "?")
	if m.mode != modeKeys {
		t.Fatalf("? should open the keys popup, mode = %v", m.mode)
	}
	for _, closer := range []string{"esc", "?", "q"} {
		m, _ = press(t, m, closer)
		if m.mode != modeList {
			t.Fatalf("%q should close the keys popup, mode = %v", closer, m.mode)
		}
		m, _ = press(t, m, "?")
	}
	m, _ = press(t, m, "j") // other keys are ignored while the popup is up
	if m.mode != modeKeys || m.cursorName() != "kitty-session" {
		t.Fatalf("j should be ignored in the keys popup: mode %v cursor %q", m.mode, m.cursorName())
	}
	m, _ = press(t, m, "esc")

	// keys is the menu entry just above quit ks.
	keys := []string{"m"}
	for range len(menuEntries) - 2 {
		keys = append(keys, "j")
	}
	m, _ = press(t, m, append(keys, "enter")...)
	if m.mode != modeKeys {
		t.Fatalf("menu keys entry should open the popup, mode = %v", m.mode)
	}
	if menuEntries[len(menuEntries)-2].label != "keys" ||
		menuEntries[len(menuEntries)-1].label != "quit ks" {
		t.Fatalf("keys must sit just above quit ks: %v", menuLabels())
	}
	wantCalls(t, fb)
}

func TestKeysPopupFitsWidth36(t *testing.T) {
	m := newTestModel(t, &fakeBackend{agents: mockupAgents()}, "")
	m, _ = press(t, m, "?")
	assertGeometry(t, m, DefaultWidth, testLines)
	got := lines(m)
	for _, k := range keyHelps {
		row := fitWidth(k.key, keyColumn) + k.action
		if !contains(got, row) {
			t.Errorf("keys row %q missing or cut:\n%s", row, strings.Join(got, "\n"))
		}
	}
	for _, chord := range []string{"ctrl+b s", "ctrl+b a", "ctrl+w w", "ctrl+w h/l"} {
		if !contains(got, chord+" ") {
			t.Errorf("kitty chord %q missing:\n%s", chord, strings.Join(got, "\n"))
		}
	}
}
