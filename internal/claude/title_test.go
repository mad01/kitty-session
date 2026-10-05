package claude

import "testing"

func TestParseTitle(t *testing.T) {
	tests := []struct {
		title    string
		state    State
		stripped string
		ok       bool
	}{
		{"✳ Claude Code", StateIdle, "Claude Code", true},
		{"✳", StateIdle, "", true},
		{"◐ Pong response", StateWorking, "Pong response", true},
		{"⠋ Reading files", StateWorking, "Reading files", true},
		{"⣿   wide gap", StateWorking, "wide gap", true},
		{"✻ Thinking", StateWorking, "Thinking", true},
		{"· dots", StateWorking, "dots", true},
		{"claude", StateUnknown, "claude", false},
		{"Claude Code ✳ trailing", StateUnknown, "Claude Code ✳ trailing", false},
		{"", StateUnknown, "", false},
		{"\xff bad utf8", StateUnknown, "\xff bad utf8", false},
	}
	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			state, stripped, ok := ParseTitle(tt.title)
			if ok != tt.ok || state != tt.state || stripped != tt.stripped {
				t.Fatalf("ParseTitle(%q) = (%v, %q, %v), want (%v, %q, %v)",
					tt.title, state, stripped, ok, tt.state, tt.stripped, tt.ok)
			}
		})
	}
}
