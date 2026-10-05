package sidebar

import (
	"strings"
	"testing"
	"time"
)

func TestSortAgents(t *testing.T) {
	t0 := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) time.Time { return t0.Add(d) }
	tests := []struct {
		name string
		in   []Agent
		want string
	}{
		{
			name: "state priority wins over creation order",
			in: []Agent{
				{Name: "stopped", State: StateStopped, CreatedAt: at(0)},
				{Name: "idle", State: StateIdle, CreatedAt: at(-time.Minute)},
				{Name: "working", State: StateWorking, CreatedAt: at(-2 * time.Minute)},
				{Name: "done", State: StateDone, CreatedAt: at(-3 * time.Minute)},
				{Name: "input", State: StateInput, CreatedAt: at(-4 * time.Minute)},
			},
			want: "input,done,working,idle,stopped",
		},
		{
			name: "ties by creation order, oldest first",
			in: []Agent{
				{Name: "old", State: StateIdle, CreatedAt: at(-time.Hour)},
				{Name: "new", State: StateIdle, CreatedAt: at(0)},
				{Name: "mid", State: StateIdle, CreatedAt: at(-time.Minute)},
			},
			want: "old,mid,new",
		},
		{
			name: "same state and time falls back to name",
			in: []Agent{
				{Name: "b", State: StateWorking, CreatedAt: at(0)},
				{Name: "a", State: StateWorking, CreatedAt: at(0)},
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
