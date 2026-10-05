package sidebar

import (
	"strings"
	"testing"
)

func TestSortAgents(t *testing.T) {
	tests := []struct {
		name string
		in   []Agent
		want string
	}{
		{
			name: "tab order wins over state",
			in: []Agent{
				{Name: "idle", State: StateIdle, Tab: 3},
				{Name: "input", State: StateInput, Tab: 2},
				{Name: "working", State: StateWorking, Tab: 1},
			},
			want: "working,input,idle",
		},
		{
			name: "no tab sorts last, by name",
			in: []Agent{
				{Name: "b-stopped", State: StateStopped},
				{Name: "tabbed", State: StateIdle, Tab: 1},
				{Name: "a-gone", State: StateStopped},
			},
			want: "tabbed,a-gone,b-stopped",
		},
		{
			name: "same tab falls back to name",
			in: []Agent{
				{Name: "b", State: StateWorking, Tab: 1},
				{Name: "a", State: StateWorking, Tab: 1},
			},
			want: "a,b",
		},
		{name: "empty", in: nil, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sortAgents(tt.in)
			if got := strings.Join(names(tt.in), ","); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStateString(t *testing.T) {
	want := map[State]string{
		StateInput: "input", StateDone: "done", StateWorking: "working",
		StateIdle: "idle", StateStopped: "stopped", State(99): "unknown",
	}
	for s, label := range want {
		if s.String() != label {
			t.Errorf("State(%d).String() = %q, want %q", int(s), s.String(), label)
		}
	}
}

func TestShortenHome(t *testing.T) {
	tests := []struct {
		dir, home, want string
	}{
		{"/home/u/code/x", "/home/u", "~/code/x"},
		{"/home/u", "/home/u", "~"},
		{"/home/user2/code/x", "/home/u", "/home/user2/code/x"},
		{"/home/u2", "/home/u", "/home/u2"},
		{"/opt/x", "/home/u", "/opt/x"},
		{"/home/u/code", "", "/home/u/code"},
	}
	for _, tt := range tests {
		if got := ShortenHome(tt.dir, tt.home); got != tt.want {
			t.Errorf("ShortenHome(%q, %q) = %q, want %q", tt.dir, tt.home, got, tt.want)
		}
	}
}
