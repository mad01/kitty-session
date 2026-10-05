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
// transcript_path. tool_name is set for PreToolUse and notification_type for
// Notification; other events leave them empty.
type hookPayload struct {
	HookEventName    string `json:"hook_event_name"`
	SessionID        string `json:"session_id"`
	TranscriptPath   string `json:"transcript_path"`
	ToolName         string `json:"tool_name"`
	NotificationType string `json:"notification_type"`
}

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

	var s string
	var refreshSummary bool
	switch payload.HookEventName {
	case "PreToolUse":
		s = "working"
		// Refresh summary on plan mode transitions
		if payload.ToolName == "EnterPlanMode" || payload.ToolName == "ExitPlanMode" {
			refreshSummary = true
		}
	case "Stop":
		s = "idle"
		refreshSummary = true
	case "Notification":
		switch payload.NotificationType {
		case "permission_prompt", "elicitation_dialog":
			s = "input"
		default:
			return nil
		}
	case "SessionStart":
		s = "waiting"
	default:
		return nil
	}

	if err := state.Write(name, s); err != nil {
		return err
	}

	if refreshSummary {
		store, err := session.NewStore()
		if err != nil {
			return nil
		}
		sess, err := store.Load(name)
		if err != nil {
			return nil
		}
		if sess.KittySummaryWindowID != 0 {
			_ = kitty.SendText(sess.KittySummaryWindowID, "refresh\n")
		}
	}

	return nil
}
