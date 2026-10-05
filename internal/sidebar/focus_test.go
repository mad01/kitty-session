package sidebar

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// withColour renders styles in true colour for the test, so a colour change
// shows up in the bytes; the default profile off a TTY emits none.
func withColour(t *testing.T) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

func TestFrameStyleFollowsFocus(t *testing.T) {
	tests := []struct {
		name    string
		focused bool
		want    lipgloss.TerminalColor
	}{
		{"blurred keeps the muted frame", false, colorMuted},
		{"focused turns the frame light green", true, colorFocus},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := frameStyle(tt.focused).GetForeground(); got != tt.want {
				t.Fatalf("frameStyle(%v) foreground = %v, want %v", tt.focused, got, tt.want)
			}
		})
	}
	if frameStyle(true).GetForeground() == frameStyle(false).GetForeground() {
		t.Fatal("focused and blurred frames share a colour")
	}
}

// stripBars returns a rendered line without its two side bars, failing when
// the line is not framed by the bar the frame style draws.
func stripBars(t *testing.T, line, bar string) string {
	t.Helper()
	if !strings.HasPrefix(line, bar) || !strings.HasSuffix(line, bar) {
		t.Fatalf("line is not framed by %q: %q", bar, line)
	}
	return strings.TrimSuffix(strings.TrimPrefix(line, bar), bar)
}

func TestFocusRecoloursOnlyTheFrame(t *testing.T) {
	withColour(t)
	fb := &fakeBackend{agents: mockupAgents()}
	blurred := newTestModel(t, fb, "kitty-session")
	focused := update(t, blurred, tea.FocusMsg{})
	if focused.View() == blurred.View() {
		t.Fatal("focus did not change the rendering")
	}
	if strings.Join(lines(focused), "\n") != strings.Join(lines(blurred), "\n") {
		t.Fatal("focus changed the text, not just its colour")
	}
	if focused.mode != blurred.mode || focused.cursor != blurred.cursor || focused.status != "" {
		t.Fatalf("focus changed more than the frame: mode %v cursor %d status %q",
			focused.mode, focused.cursor, focused.status)
	}
	bl := strings.Split(blurred.View(), "\n")
	fl := strings.Split(focused.View(), "\n")
	mutedBar, focusBar := frameStyle(false).Render("│"), frameStyle(true).Render("│")
	for i := 1; i < len(bl)-1; i++ {
		if stripBars(t, bl[i], mutedBar) != stripBars(t, fl[i], focusBar) {
			t.Fatalf("line %d content changed with focus", i)
		}
	}
	again := update(t, focused, tea.BlurMsg{})
	if again.View() != blurred.View() {
		t.Fatal("blur did not restore the muted frame")
	}
}

func TestFocusSeedAppliesUnlessAnEventCameFirst(t *testing.T) {
	fb := &fakeBackend{agents: mockupAgents(), focused: true}
	m := newTestModel(t, fb, "")
	m, _ = feed(t, m, m.focusedCmd())
	if !m.focused {
		t.Fatal("seed did not mark the sidebar focused")
	}
	// A blur that arrived first is newer than the snapshot and wins.
	m = update(t, m, tea.BlurMsg{})
	m, _ = feed(t, m, m.focusedCmd())
	if m.focused {
		t.Fatal("stale seed overrode a blur event")
	}

	fb.focusErr = errors.New("ls failed\nsecond line")
	m = newTestModel(t, fb, "")
	m, _ = feed(t, m, m.focusedCmd())
	if m.focused || !m.statusErr || m.status != "ls failed" {
		t.Fatalf("failed seed: focused %v statusErr %v status %q",
			m.focused, m.statusErr, m.status)
	}
}

func TestInitSeedsFocusFromTheBackend(t *testing.T) {
	fb := &fakeBackend{focused: true}
	m := newModel(Options{Width: DefaultWidth, Backend: fb}, "/home/u")
	for _, msg := range drain(m.Init()) {
		if seed, ok := msg.(focusMsg); ok {
			if !seed.focused || seed.err != nil {
				t.Fatalf("seed = %+v, want focused without error", seed)
			}
			return
		}
	}
	t.Fatal("Init issued no focus seed")
}
