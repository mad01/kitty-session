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

// hookPayload is the JSON structure Claude Code sends to hook commands via stdin.
type hookPayload struct {
	Event string `json:"event"`
	Tool  struct {
		Name string `json:"name"`
	} `json:"tool"`
	Notification struct {
		Type string `json:"type"`
	} `json:"notification"`
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

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("cannot read stdin: %w", err)
	}

	var payload hookPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return fmt.Errorf("cannot parse hook payload: %w", err)
	}

	var s string
	var refreshSummary bool
	switch payload.Event {
	case "PreToolUse":
		s = "working"
		// Refresh summary on plan mode transitions
		if payload.Tool.Name == "EnterPlanMode" || payload.Tool.Name == "ExitPlanMode" {
			refreshSummary = true
		}
	case "Stop":
		s = "idle"
		refreshSummary = true
	case "Notification":
		switch payload.Notification.Type {
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
