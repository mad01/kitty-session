package claude

import "testing"

func TestDetectState(t *testing.T) {
	tests := []struct {
		name string
		text string
		want State
	}{
		{
			name: "idle prompt",
			text: "Some previous output\n\n>\n",
			want: StateIdle,
		},
		{
			name: "idle prompt no trailing newline",
			text: "Some previous output\n\n>",
			want: StateIdle,
		},
		{
			name: "permission prompt y/n",
			text: "Claude wants to run a command\nAllow? (y/n)\n",
			want: StateNeedsInput,
		},
		{
			name: "allow with yes/no",
			text: "Do you allow this action?\nAllow  Yes  No\n",
			want: StateNeedsInput,
		},
		{
			name: "do you want to",
			text: "Do you want to continue with this change?\n",
			want: StateNeedsInput,
		},
		{
			name: "working state with tool output",
			text: "Reading file foo.go...\nAnalyzing code...\n",
			want: StateWorking,
		},
		{
			name: "working state with spinner",
			text: "⠙ Processing files...\n",
			want: StateWorking,
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
			name: "prompt with text after chevron",
			text: "Some output\n> some user input",
			want: StateIdle,
		},
		{
			name: "approve prompt",
			text: "Claude wants to edit a file\nApprove? Yes / No\n",
			want: StateNeedsInput,
		},
		{
			name: "welcome screen",
			text: " ╭──────────────────────────────────╮\n │ ✻ Welcome to Claude Code!       │\n │                                  │\n │ /help for help                   │\n ╰──────────────────────────────────╯\n",
			want: StateWaiting,
		},
		{
			name: "claude finished no prompt yet",
			text: "Here is the summary of changes I made:\n- Updated foo.go\n- Fixed the bug\n",
			want: StateIdle,
		},
		{
			name: "claude actively building",
			text: "Building the project...\nCompiling main.go\n",
			want: StateWorking,
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
