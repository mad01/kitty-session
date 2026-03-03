package claude

import (
	"strings"
)

// State represents the current state of a Claude Code session.
type State int

const (
	StateUnknown    State = iota
	StateStopped          // tab no longer exists
	StateWorking          // Claude is actively processing
	StateNeedsInput       // Claude needs user input (permission, question)
	StateIdle             // at prompt, ready for new task
	StateWaiting          // session starting up or Claude finished, ready for next task
)

// String returns a human-readable label for the state.
func (s State) String() string {
	switch s {
	case StateStopped:
		return "stopped"
	case StateWorking:
		return "working"
	case StateNeedsInput:
		return "input"
	case StateIdle:
		return "idle"
	case StateWaiting:
		return "waiting"
	default:
		return "unknown"
	}
}

// workingSignals are substrings that indicate Claude is actively processing.
var workingSignals = []string{
	"reading", "writing", "editing", "searching",
	"running", "executing", "analyzing", "creating",
	"updating", "installing", "building", "compiling",
	"fetching", "downloading",
	"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏", // spinner characters
}

// DetectState scans terminal text and determines what Claude Code is doing.
// It looks at the last ~50 non-empty lines from the bottom up.
func DetectState(text string) State {
	lines := strings.Split(text, "\n")

	// Collect last 50 non-empty lines (bottom-up)
	var tail []string
	for i := len(lines) - 1; i >= 0 && len(tail) < 50; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed != "" {
			tail = append(tail, trimmed)
		}
	}

	// Empty terminal — session is still loading
	if len(tail) == 0 {
		return StateWaiting
	}

	// Idle: last non-empty line is just ">" (Claude's input prompt)
	if tail[0] == ">" {
		return StateIdle
	}

	// Check bottom-up for signals
	for _, line := range tail {
		lower := strings.ToLower(line)

		// NeedsInput: permission prompts, y/n questions
		if strings.Contains(lower, "(y/n)") {
			return StateNeedsInput
		}
		if (strings.Contains(lower, "allow") || strings.Contains(lower, "approve")) &&
			(strings.Contains(lower, "yes") || strings.Contains(lower, "no")) {
			return StateNeedsInput
		}
		if strings.Contains(lower, "do you want to") {
			return StateNeedsInput
		}
	}

	// Welcome/startup screen — Claude hasn't been given a task yet
	for _, line := range tail {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "welcome to claude") ||
			strings.Contains(lower, "/help for help") {
			return StateWaiting
		}
	}

	// Look for active work signals
	for _, line := range tail {
		lower := strings.ToLower(line)
		for _, signal := range workingSignals {
			if strings.Contains(lower, signal) {
				return StateWorking
			}
		}
	}

	// No work signals found — Claude is likely done or between tasks
	return StateWaiting
}
