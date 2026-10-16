package claude

import (
	"strings"
	"testing"
)

// Synthesized screens in the shapes Claude Code 2.1 draws: the flat prompt
// box between two horizontal rules, dialogs with a footer of key hints.
const (
	promptBoxText = `⏺ Done. The tests pass.

─────────────────────────────────────────────────────────────
❯
─────────────────────────────────────────────────────────────
  ⏵⏵ accept edits on (shift+tab to cycle)
`

	bashDialogText = `⏺ Bash(make test)
  ⎿  Running…

─────────────────────────────────────────────────────────────
 Bash command

   make test
   Run the test suite

 Do you want to proceed?
 ❯ 1. Yes
   2. Yes, and don't ask again for: make test
   3. No, and tell Claude what to do differently (esc)

 Enter to confirm · Esc to cancel
`

	bashDialogCursorOnNoText = `─────────────────────────────────────────────────────────────
 Bash command

   make test
   Run the test suite

 Do you want to proceed?
   1. Yes
   2. Yes, and don't ask again for: make test
 ❯ 3. No, and tell Claude what to do differently (esc)

 Enter to confirm · Esc to cancel
`

	editDialogText = `─────────────────────────────────────────────────────────────
 Edit file

 internal/cli/list.go
   - st := claude.DetectState(text)
   + st := claude.DetectState(claude.LiveScreen(text, w.Lines))

 Do you want to make this edit to list.go?
 ❯ 1. Yes
   2. Yes, allow all edits during this session (shift+tab)
   3. No, and tell Claude what to do differently (esc)

 Enter to confirm · Esc to cancel
`

	questionMenuText = `─────────────────────────────────────────────────────────────
 Which approach should I take?

 ❯ 1. Keep the seam
   2. Add a writer interface
   3. Type something.

 Enter to select · ↑/↓ to navigate · Esc to cancel
`

	planApprovalText = `─────────────────────────────────────────────────────────────
 Ready to code?

 Here is Claude's plan:
 ┌───────────────────────────────────────┐
 │ 1. Add GetTextAll to the kitty client │
 │ 2. Parse the screen                   │
 └───────────────────────────────────────┘

 Would you like to proceed?
 ❯ 1. Yes, and auto-accept edits
   2. Yes, and manually approve edits
   3. No, keep planning

 Enter to confirm · Esc to cancel
`

	elicitationText = `─────────────────────────────────────────────────────────────
 MCP server "linear" requests your input

 Which team?
 ❯ mad01

 ❯ Accept   Decline

 Esc to cancel
`

	trustDialogText = `─────────────────────────────────────────────────────────────
 Do you trust the files in this folder?

 /tmp/smoke

 Claude Code may read files in this folder. Reading untrusted
 files may lead Claude Code to behave in unexpected ways.

 ❯ 1. Yes, proceed
   2. No, exit

 Enter to confirm · Esc to exit
`

	trustDialogNoFooterText = `─────────────────────────────────────────────────────────────
 Do you trust the files in this folder?

 /tmp/smoke

 ❯ 1. Yes, proceed
   2. No, exit
`

	workingTurnText = `⏺ Bash(go test ./...)
  ⎿  Running…

✻ Crunching… (12s · ↓ 1.4k tokens · esc to interrupt)

  ⏵⏵ accept edits on (shift+tab to cycle) · esc to interrupt
`

	workingWithPromptBoxText = `⏺ Bash(go test ./...)
  ⎿  Running…

─────────────────────────────────────────────────────────────
❯
─────────────────────────────────────────────────────────────
  ⏵⏵ accept edits on (shift+tab to cycle) · esc to interrupt
`

	backgroundAgentsText = `⏺ Agent(screen-impl)
  ⎿  Running…

· Waiting for 2 background agents to finish

─────────────────────────────────────────────────────────────
❯
─────────────────────────────────────────────────────────────
`

	transcriptViewerText = `⏺ Read(internal/cli/list.go)
  ⎿  Read 140 lines

⏺ The list command reads the pane text through a seam.

Showing detailed transcript · Ctrl+O to toggle
`

	scrolledBackText = `⏺ Read(internal/cli/list.go)
  ⎿  Read 140 lines

⏺ I'll change the paneText seam next.

⏺ Update(internal/cli/list.go)
  ⎿  Updated internal/cli/list.go with 2 additions
`

	dialogBodyText = `─────────────────────────────────────────────────────────────
 Do you want to proceed?
 ❯ 1. Yes
   2. No
─────────────────────────────────────────────────────────────
`

	// leftoverSpinnerText is a short live screen (promptBoxText, six rows)
	// under a spinner row Ink left in the scrollback after /clear.
	leftoverSpinnerText = "✻ Thinking… (3s · esc to interrupt)\n\n\n" + promptBoxText

	modelPickerText = ` Select model

 ❯ 1. Default (recommended)
   2. Opus
   3. Sonnet

 Enter to set as default · Esc to cancel
`
)

func TestParseScreen(t *testing.T) {
	tests := []struct {
		name string
		text string
		want Screen
	}{
		{"bash permission dialog, cursor on yes", bashDialogText, Screen{Blocker: true}},
		{"bash permission dialog, cursor on no", bashDialogCursorOnNoText, Screen{Blocker: true}},
		{"edit permission dialog", editDialogText, Screen{Blocker: true}},
		{"question menu", questionMenuText, Screen{Blocker: true}},
		{"plan approval", planApprovalText, Screen{Blocker: true}},
		{"mcp elicitation", elicitationText, Screen{Blocker: true}},
		{"trust dialog", trustDialogText, Screen{Blocker: true}},
		{"trust dialog without a footer", trustDialogNoFooterText, Screen{Blocker: true}},
		{"prompt box at rest", promptBoxText, Screen{PromptBox: true}},
		{"running turn", workingTurnText, Screen{Working: true}},
		{
			"running turn with the prompt box drawn",
			workingWithPromptBoxText,
			Screen{PromptBox: true, Working: true},
		},
		{
			"waiting for background agents",
			backgroundAgentsText,
			Screen{PromptBox: true, Working: true},
		},
		{"braille spinner", "⠹ Reading internal/cli/list.go\n", Screen{Working: true}},
		{"half-circle spinner", "◐ Pondering… (4s · esc to interrupt)\n", Screen{Working: true}},
		{"ascii spinner", "* Pondering…\n", Screen{Working: true}},
		{"transcript viewer", transcriptViewerText, Screen{}},
		{"scrolled-back chunk", scrolledBackText, Screen{}},
		{"model picker", modelPickerText, Screen{}},
		{"cursor inside a dialog body is not the prompt", dialogBodyText, Screen{Blocker: true}},
		{
			"dialog below a running turn",
			workingTurnText + bashDialogText,
			Screen{Blocker: true, Working: true},
		},
		{"empty", "", Screen{}},
		{"whitespace", "  \n\t\n", Screen{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseScreen(tt.text); got != tt.want {
				t.Errorf("ParseScreen = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestParseScreenReadsTheLiveScreen checks that a spinner row above a short
// live screen counts only while the text is not cut to the screen's rows.
func TestParseScreenReadsTheLiveScreen(t *testing.T) {
	const rows = 6 // promptBoxText's height
	cut := LiveScreen(leftoverSpinnerText, rows)
	if cut != promptBoxText {
		t.Fatalf("LiveScreen cut to %q, want the prompt box", cut)
	}
	if got := ParseScreen(cut); got != (Screen{PromptBox: true}) {
		t.Errorf("cut: ParseScreen = %+v, want the prompt box only", got)
	}
	if got := ParseScreen(leftoverSpinnerText); got != (Screen{PromptBox: true, Working: true}) {
		t.Errorf("uncut: ParseScreen = %+v, want prompt box and working", got)
	}
}

func TestLiveScreen(t *testing.T) {
	tests := []struct {
		name string
		text string
		rows int
		want string
	}{
		{"zero rows keeps everything", "a\nb\nc\n", 0, "a\nb\nc\n"},
		{"fewer lines than rows", "a\nb\n", 5, "a\nb\n"},
		{"last rows lines", "a\nb\nc\nd\n", 2, "c\nd\n"},
		{"empty lines count as rows", "a\n\n\nd\n", 3, "\n\nd\n"},
		{"no trailing newline", "a\nb\nc", 2, "b\nc"},
		{"empty text", "", 3, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LiveScreen(tt.text, tt.rows); got != tt.want {
				t.Errorf("LiveScreen(%q, %d) = %q, want %q", tt.text, tt.rows, got, tt.want)
			}
		})
	}
}

// TestParseScreenWorkingReadsTheLastLines checks that a working signal counts
// only inside the last workingTail non-empty lines, while a blocker further
// up, inside screenTail, still does.
func TestParseScreenWorkingReadsTheLastLines(t *testing.T) {
	spinner := "⠹ Reading internal/cli/list.go\n"
	inside := spinner + strings.Repeat("⏺ more output\n", workingTail-1)
	if got := ParseScreen(inside); !got.Working {
		t.Errorf("spinner inside the last %d lines: ParseScreen = %+v, want working",
			workingTail, got)
	}
	outside := spinner + strings.Repeat("⏺ more output\n", workingTail)
	if got := ParseScreen(outside); got.Working {
		t.Errorf("spinner above the last %d lines: ParseScreen = %+v, want not working",
			workingTail, got)
	}
	dialog := bashDialogText + strings.Repeat("⏺ more output\n", workingTail)
	if got := ParseScreen(dialog); !got.Blocker {
		t.Errorf("dialog above the last %d lines: ParseScreen = %+v, want blocker",
			workingTail, got)
	}
}

// TestParseScreenReadsTheTailOnly checks that a dialog pushed out of the
// last screenTail non-empty lines by later output no longer counts.
func TestParseScreenReadsTheTailOnly(t *testing.T) {
	filler := strings.Repeat("⏺ more output\n", screenTail)
	if got := ParseScreen(bashDialogText + filler); got != (Screen{}) {
		t.Errorf("dialog above the tail: ParseScreen = %+v, want nothing", got)
	}
	if got := ParseScreen(filler + promptBoxText); !got.PromptBox {
		t.Errorf("prompt box under the filler: ParseScreen = %+v, want prompt box", got)
	}
}

func TestIsRule(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{"─────", true},
		{"  ━━━━━━  ", true},
		{"╭──────────╮", true},
		{"──", false},
		{"", false},
		{"─ a ─ b ─ c ─", false},
		{"❯ ", false},
	}
	for _, tt := range tests {
		if got := isRule(tt.line); got != tt.want {
			t.Errorf("isRule(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}
