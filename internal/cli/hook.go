package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/mad01/kitty-session/internal/kitty"
	"github.com/mad01/kitty-session/internal/session"
	"github.com/mad01/kitty-session/internal/state"
	"github.com/spf13/cobra"
)

// hookPayload is the subset of the JSON object Claude Code pipes to hook
// commands on stdin. Every event carries hook_event_name, session_id and
// transcript_path. tool_name is set for PreToolUse, notification_type for
// Notification and reason for SessionEnd; other events leave them empty.
type hookPayload struct {
	HookEventName    string `json:"hook_event_name"`
	SessionID        string `json:"session_id"`
	TranscriptPath   string `json:"transcript_path"`
	ToolName         string `json:"tool_name"`
	NotificationType string `json:"notification_type"`
	Reason           string `json:"reason"`
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

func runHook(cmd *cobra.Command, args []string) error {
	name := os.Getenv("KS_SESSION_NAME")
	if name == "" {
		return nil // not inside a ks session, nothing to do
	}

	data, err := io.ReadAll(cmd.InOrStdin())
	if err != nil {
		return fmt.Errorf("cannot read stdin: %w", err)
	}

	var payload hookPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return fmt.Errorf("cannot parse hook payload: %w", err)
	}

	if payload.HookEventName == "SessionEnd" {
		markSessionStopped(name, payload.Reason)
		return nil
	}

	s, refreshSummary := stateForEvent(payload)
	if s == "" {
		return nil
	}
	if err := state.Write(name, s); err != nil {
		return err
	}

	// Everything below is best effort: the state file is the hook's job, and
	// a missing or unreadable session record must not fail Claude's hook run.
	if payload.HookEventName == "SessionStart" {
		recordClaudeSession(name, payload.SessionID)
	}
	if refreshSummary {
		refreshSummaryTab(name)
	}
	return nil
}

// stateForEvent maps a hook event to the state ks records for it, or "" when
// ks ignores the event. The bool says whether the summary tab should refresh.
func stateForEvent(p hookPayload) (string, bool) {
	switch p.HookEventName {
	case "PreToolUse":
		// Refresh summary on plan mode transitions
		planMode := p.ToolName == "EnterPlanMode" || p.ToolName == "ExitPlanMode"
		return "working", planMode
	case "Stop":
		return "idle", true
	case "Notification":
		switch p.NotificationType {
		case "permission_prompt", "elicitation_dialog":
			return "input", false
		}
		return "", false
	case "SessionStart":
		return "waiting", false
	}
	return "", false
}

// recordClaudeSession stores Claude's session_id on the ks session so a later
// reopen can claude --resume it, and marks the session active again.
func recordClaudeSession(name, claudeSessionID string) {
	if claudeSessionID == "" {
		return
	}
	store, sess, err := loadSession(name)
	if err != nil {
		return
	}
	sess.ClaudeSessionID = claudeSessionID
	sess.Status = session.StatusActive
	_ = store.Save(sess)
}

// markSessionStopped records that the user ended the agent. Only an explicit
// exit counts; a window closed by kitty (reason other) or a /clear or
// /resume restart leaves the session active.
func markSessionStopped(name, reason string) {
	if reason != reasonPromptInputExit && reason != reasonLogout {
		return
	}
	store, sess, err := loadSession(name)
	if err != nil {
		return
	}
	sess.Status = session.StatusStopped
	_ = store.Save(sess)
}

// refreshSummaryTab nudges the session's Haiku summary tab, if it has one.
func refreshSummaryTab(name string) {
	_, sess, err := loadSession(name)
	if err != nil {
		return
	}
	if sess.KittySummaryWindowID != 0 {
		_ = kitty.SendText(sess.KittySummaryWindowID, "refresh\n")
	}
}

// loadSession opens the store and loads the named session record.
func loadSession(name string) (*session.Store, *session.Session, error) {
	store, err := session.NewStore()
	if err != nil {
		return nil, nil, err
	}
	sess, err := store.Load(name)
	if err != nil {
		return nil, nil, err
	}
	return store, sess, nil
}
