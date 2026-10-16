package claude

import (
	"strings"
	"testing"
)

func TestDetectState(t *testing.T) {
	tests := []struct {
		name string
		text string
		want State
	}{
		{
			name: "prompt box at rest",
			text: "Some previous output\n" + promptBoxText,
			want: StateIdle,
		},
		{
			name: "prompt box no trailing newline",
			text: strings.TrimRight(promptBoxText, "\n"),
			want: StateIdle,
		},
		{
			name: "a bare chevron is not the prompt box",
			text: "Some previous output\n\n>\n",
			want: StateWaiting,
		},
		{
			name: "bash permission dialog",
			text: bashDialogText,
			want: StateNeedsInput,
		},
		{
			name: "question menu",
			text: questionMenuText,
			want: StateNeedsInput,
		},
		{
			name: "a y/n line alone is no dialog",
			text: "Claude wants to run a command\nAllow? (y/n)\n",
			want: StateWaiting,
		},
		{
			name: "running turn",
			text: workingTurnText,
			want: StateWorking,
		},
		{
			name: "braille spinner",
			text: "⠙ Processing files...\n",
			want: StateWorking,
		},
		{
			name: "activity words alone are no signal",
			text: "Reading file foo.go...\nAnalyzing code...\n",
			want: StateWaiting,
		},
		{
			name: "empty text",
			text: "",
			want: StateWaiting,
		},
		{
			name: "only whitespace",
			text: "   \n  \n\n  ",
			want: StateWaiting,
		},
		{
			name: "welcome screen",
			text: " ╭──────────────────────────────────╮\n │ ✻ Welcome to Claude Code!       │\n │                                  │\n │ /help for help                   │\n ╰──────────────────────────────────╯\n",
			want: StateWaiting,
		},
		{
			name: "claude finished no prompt yet",
			text: "Here is the summary of changes I made:\n- Updated foo.go\n- Fixed the bug\n",
			want: StateWaiting,
		},
		{
			name: "dialog over a running turn is input",
			text: workingTurnText + "\n" + bashDialogText,
			want: StateNeedsInput,
		},
		{
			name: "transcript viewer leaves the state alone",
			text: transcriptViewerText,
			want: StateWaiting,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectState(tt.text)
			if got != tt.want {
				t.Errorf("DetectState() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseState(t *testing.T) {
	states := []State{StateWorking, StateIdle, StateNeedsInput, StateWaiting, StateError}
	for _, st := range states {
		if got := ParseState(st.String()); got != st {
			t.Errorf("ParseState(%q) = %v, want %v", st.String(), got, st)
		}
	}
	for _, s := range []string{"", "stopped", "bogus"} {
		if got := ParseState(s); got != StateUnknown {
			t.Errorf("ParseState(%q) = %v, want StateUnknown", s, got)
		}
	}
}
