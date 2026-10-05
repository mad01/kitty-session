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
			name: "state priority wins over recency",
			in: []Agent{
				{Name: "stopped", State: StateStopped, ChangedAt: at(0)},
				{Name: "idle", State: StateIdle, ChangedAt: at(-time.Minute)},
				{Name: "working", State: StateWorking, ChangedAt: at(-2 * time.Minute)},
				{Name: "done", State: StateDone, ChangedAt: at(-3 * time.Minute)},
				{Name: "input", State: StateInput, ChangedAt: at(-4 * time.Minute)},
			},
			want: "input,done,working,idle,stopped",
		},
		{
			name: "ties by most recent change first",
			in: []Agent{
				{Name: "old", State: StateIdle, ChangedAt: at(-time.Hour)},
				{Name: "new", State: StateIdle, ChangedAt: at(0)},
				{Name: "mid", State: StateIdle, ChangedAt: at(-time.Minute)},
			},
			want: "new,mid,old",
		},
		{
			name: "same state and time falls back to name",
			in: []Agent{
				{Name: "b", State: StateWorking, ChangedAt: at(0)},
				{Name: "a", State: StateWorking, ChangedAt: at(0)},
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
