package sidebar

import (
	"testing"
	"time"
)

// withoutOwn clears the Own flag on every agent.
func withoutOwn(agents []Agent) []Agent {
	for i := range agents {
		agents[i].Own = false
	}
	return agents
}

func TestFirstLoadCursorDefaultsToOwn(t *testing.T) {
	tests := []struct {
		name   string
		agents []Agent
		filter string
		want   string
	}{
		{"own present", mockupAgents(), "", "kitty-session"},
		{"own absent falls back to row 0", withoutOwn(mockupAgents()), "", "thismoon"},
		{"own filtered out falls back to row 0", mockupAgents(), "dot", "dotfiles"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fb := &fakeBackend{agents: tt.agents}
			m := newModel(Options{Width: DefaultWidth, Backend: fb}, "/home/u")
			m.filter.SetValue(tt.filter)
			m = load(t, m)
			if got := m.cursorName(); got != tt.want {
				t.Fatalf("cursor on %q, want %q", got, tt.want)
			}
			if m.navigated {
				t.Fatal("first load must not count as navigation")
			}
		})
	}
}

func TestFocusSnapsCursorToOwn(t *testing.T) {
	tests := []struct {
		name       string
		agents     []Agent
		moves      []string // cursor keys before enter
		wantFocus  string
		wantCursor string
	}{
		{
			"non-own agent snaps back to own", mockupAgents(),
			[]string{"k", "k", "k", "k"},
			"thismoon", "kitty-session",
		},
		{"own agent just focuses", mockupAgents(), nil, "kitty-session", "kitty-session"},
		{
			"non-own agent without an own row snaps to row 0", withoutOwn(mockupAgents()),
			[]string{"j"},
			"dropbrain-app", "thismoon",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fb := &fakeBackend{agents: tt.agents}
			m := newTestModel(t, fb, "")
			m, _ = press(t, m, tt.moves...)
			m.setStatus("keep me")
			next, cmd := m.focusCursor()
			m = asModel(t, next)
			if got := m.cursorName(); got != tt.wantCursor {
				t.Fatalf("cursor on %q right after dispatch, want %q", got, tt.wantCursor)
			}
			if m.status != "keep me" {
				t.Fatalf("snap must keep the status line, got %q", m.status)
			}
			m, _ = feed(t, m, cmd) // Focus runs, then the refresh is issued
			wantCalls(t, fb, "focus:"+tt.wantFocus)
			m = load(t, m)
			if got := m.cursorName(); got != tt.wantCursor {
				t.Fatalf("cursor on %q after the refresh, want %q", got, tt.wantCursor)
			}
		})
	}
}

func TestResortKeepsCursorOnOwnUnlessNavigated(t *testing.T) {
	t.Run("not navigated: cursor follows the own row as it moves", func(t *testing.T) {
		fb := &fakeBackend{agents: mockupAgents()}
		m := newTestModel(t, fb, "")
		if m.cursor != 4 {
			t.Fatalf("own row at %d, want 4", m.cursor)
		}
		fb.agents[0].State = StateInput // kitty-session jumps to the top
		fb.agents[0].ChangedAt = testNow
		m = load(t, m)
		if m.cursor != 0 || m.cursorName() != "kitty-session" {
			t.Fatalf("cursor %d on %q, want 0 on kitty-session", m.cursor, m.cursorName())
		}
	})

	t.Run("navigated: cursor follows the agent the user picked", func(t *testing.T) {
		fb := &fakeBackend{agents: mockupAgents()}
		m := newTestModel(t, fb, "")
		m, _ = press(t, m, "k") // migraine-me
		fb.agents[1].State = StateDone
		fb.agents[1].ChangedAt = testNow
		m = load(t, m)
		if m.cursor != 1 || m.cursorName() != "migraine-me" {
			t.Fatalf("cursor %d on %q, want 1 on migraine-me", m.cursor, m.cursorName())
		}
	})

	t.Run("navigated agent gone: snap to own", func(t *testing.T) {
		fb := &fakeBackend{agents: mockupAgents()}
		m := newTestModel(t, fb, "")
		m, _ = press(t, m, "j") // dotfiles
		fb.agents = fb.agents[:5]
		m = load(t, m)
		if m.cursorName() != "kitty-session" || m.navigated {
			t.Fatalf("cursor on %q navigated %v, want own row", m.cursorName(), m.navigated)
		}
	})

	t.Run("follow after rename sticks across re-sorts", func(t *testing.T) {
		fb := &fakeBackend{agents: mockupAgents()}
		m := newTestModel(t, fb, "")
		m, _ = press(t, m, "k", "k", "k", "k") // thismoon
		m, refresh := run(t, m, "r", "-2", "enter")
		fb.agents[3].Name = "thismoon-2"
		m, _ = feed(t, m, refresh)
		if m.cursorName() != "thismoon-2" {
			t.Fatalf("cursor on %q after rename, want thismoon-2", m.cursorName())
		}
		fb.agents[3].State = StateIdle // drops below the own row
		fb.agents[3].ChangedAt = testNow.Add(-time.Hour)
		m = load(t, m)
		if m.cursorName() != "thismoon-2" {
			t.Fatalf("cursor on %q after re-sort, want thismoon-2", m.cursorName())
		}
	})
}
