package claude

// State represents the current state of a Claude Code session.
type State int

const (
	StateUnknown    State = iota
	StateStopped          // tab no longer exists
	StateWorking          // Claude is actively processing
	StateNeedsInput       // Claude needs user input (permission, question)
	StateIdle             // at prompt, ready for new task
	StateWaiting          // session starting up or Claude finished, ready for next task
	StateError            // the turn died on an error (the StopFailure hook)
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
	case StateError:
		return "error"
	default:
		return "unknown"
	}
}

// ParseState converts a state-file string ("working", "idle", "input",
// "waiting", "error") back to a State; anything else is StateUnknown.
func ParseState(s string) State {
	switch s {
	case "working":
		return StateWorking
	case "idle":
		return StateIdle
	case "input":
		return StateNeedsInput
	case "waiting":
		return StateWaiting
	case "error":
		return StateError
	default:
		return StateUnknown
	}
}

// DetectState classifies a window's terminal text for ks list, from the
// bottom of the text as ParseScreen reads it. A dialog (Screen.Blocker) is
// StateNeedsInput, a running turn (Screen.Working) StateWorking, the prompt
// box at rest (Screen.PromptBox) StateIdle. Anything else is StateWaiting
// rather than StateIdle: empty text while the session loads, the welcome
// screen before the prompt box is drawn, the gap between two tool calls.
// So a session is never advertised as finished on a screen that does not
// show its prompt. The text must end with the live screen of the window
// (kitty's get-text with --extent all, cut to the window's rows with
// LiveScreen): the scrolled-back viewport of a window shows none of the
// signals.
func DetectState(text string) State {
	s := ParseScreen(text)
	switch {
	case s.Blocker:
		return StateNeedsInput
	case s.Working:
		return StateWorking
	case s.PromptBox:
		return StateIdle
	}
	return StateWaiting
}
