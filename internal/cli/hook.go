package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/mad01/kitty-session/internal/procinfo"
	"github.com/mad01/kitty-session/internal/session"
	"github.com/mad01/kitty-session/internal/state"
	"github.com/spf13/cobra"
)

// hookPayload is the subset of the JSON object Claude Code pipes to hook
// commands on stdin. Every event carries hook_event_name, session_id and
// transcript_path. notification_type is set for Notification and reason for
// SessionEnd; other events leave them empty.
type hookPayload struct {
	HookEventName    string `json:"hook_event_name"`
	SessionID        string `json:"session_id"`
	TranscriptPath   string `json:"transcript_path"`
	NotificationType string `json:"notification_type"`
	Reason           string `json:"reason"`
	// AgentID is set when an in-process subagent fired the event. Its
	// activity still counts as state, but it must not touch the record.
	AgentID string `json:"agent_id"`
}

// SessionEnd reasons that mean the user stopped the agent on purpose. The
// others (clear, resume, other) are restarts or the window going away.
const (
	reasonPromptInputExit = "prompt_input_exit"
	reasonLogout          = "logout"
)

var hookCmd = &cobra.Command{
	Use:    "_hook",
	Short:  "Handle Claude Code hook events",
	Hidden: true,
	RunE:   runHook,
}

func init() {
	rootCmd.AddCommand(hookCmd)
}

// runHook handles one Claude Code hook event. It exits 0 whatever happens to
// the session record: the state file is the hook's job, and a record problem
// must not fail Claude's hook run. Such problems go to stderr instead.
func runHook(cmd *cobra.Command, args []string) error {
	name := os.Getenv("KS_SESSION_NAME")
	if name == "" {
		return nil // not inside a ks session, nothing to do
	}
	stderr := cmd.ErrOrStderr()
	top, err := firedByTopLevelClaude(hookProcess, os.Getppid())
	if err != nil && !errors.Is(err, procinfo.ErrUnsupported) {
		hookWarn(stderr, err) // undecidable: a stray write beats a dead badge
	}
	if err == nil && !top {
		return nil // a claude nested inside the session, not the one ks launched
	}

	payload, err := readHookPayload(cmd.InOrStdin())
	if err != nil {
		return err
	}
	isEnd := payload.HookEventName == "SessionEnd"
	s := stateForEvent(payload)
	if !isEnd && s == "" {
		return nil
	}

	store, sess := loadHookSession(stderr, os.Getenv("KS_SESSION_ID"), name)
	if sess != nil {
		name = sess.Name // state files follow the record's current name
	}
	if isEnd {
		if sess != nil && payload.AgentID == "" {
			markSessionStopped(stderr, store, sess, payload.Reason)
		}
		return nil
	}
	if err := state.Write(name, s); err != nil {
		hookWarn(stderr, err)
	}
	if sess == nil {
		return nil
	}
	if payload.HookEventName == "SessionStart" && payload.AgentID == "" {
		recordClaudeSession(stderr, store, sess, payload)
	}
	return nil
}

func readHookPayload(r io.Reader) (hookPayload, error) {
	var payload hookPayload
	data, err := io.ReadAll(r)
	if err != nil {
		return payload, fmt.Errorf("cannot read stdin: %w", err)
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return payload, fmt.Errorf("cannot parse hook payload: %w", err)
	}
	return payload, nil
}

// stateForEvent maps a hook event to the state ks records for it, or "" when
// ks ignores the event.
func stateForEvent(p hookPayload) string {
	switch p.HookEventName {
	case "PreToolUse":
		return "working"
	case "Stop":
		return "idle"
	case "Notification":
		switch p.NotificationType {
		case "permission_prompt", "elicitation_dialog":
			return "input"
		}
		return ""
	case "SessionStart":
		return "waiting"
	}
	return ""
}

// loadHookSession returns the record the event belongs to, or a nil session
// with the reason on stderr. Lookup is by KS_SESSION_ID first, so a renamed
// session still finds its record, then by KS_SESSION_NAME for records that
// predate the id.
func loadHookSession(stderr io.Writer, id, name string) (*session.Store, *session.Session) {
	store, err := session.NewStore()
	if err != nil {
		hookWarn(stderr, err)
		return nil, nil
	}
	if id != "" {
		if sess, err := store.FindByID(id); err == nil {
			return store, sess
		}
	}
	sess, err := store.Load(name)
	if err != nil {
		hookWarn(stderr, err)
		return store, nil
	}
	return store, sess
}

// recordClaudeSession stores Claude's session_id and transcript path on the
// ks session so a later reopen can claude --resume it, and marks the session
// active again.
func recordClaudeSession(
	stderr io.Writer,
	store *session.Store,
	sess *session.Session,
	p hookPayload,
) {
	if p.SessionID == "" {
		return
	}
	sess.ClaudeSessionID = p.SessionID
	sess.ClaudeTranscriptPath = p.TranscriptPath
	sess.Status = session.StatusActive
	if err := store.Save(sess); err != nil {
		hookWarn(stderr, err)
	}
}

// markSessionStopped records that the user ended the agent and drops the
// session's state file. Only an explicit exit counts; a window closed by
// kitty (reason other) or a /clear or /resume restart leaves the session
// active.
func markSessionStopped(
	stderr io.Writer,
	store *session.Store,
	sess *session.Session,
	reason string,
) {
	if reason != reasonPromptInputExit && reason != reasonLogout {
		return
	}
	sess.Status = session.StatusStopped
	if err := store.Save(sess); err != nil {
		hookWarn(stderr, err)
	}
	state.Clean(sess.Name)
}

// hookWarn reports a non-fatal hook problem on stderr.
func hookWarn(stderr io.Writer, err error) {
	fmt.Fprintf(stderr, "ks _hook: %v\n", err)
}
