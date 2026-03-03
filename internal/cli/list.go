package cli

import (
	"fmt"

	"github.com/mad01/kitty-session/internal/claude"
	"github.com/mad01/kitty-session/internal/kitty"
	"github.com/mad01/kitty-session/internal/session"
	"github.com/mad01/kitty-session/internal/state"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all sessions",
	Long:  "Show all sessions with running/stopped status.",
	RunE:  runList,
}

func init() {
	rootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, args []string) error {
	store, err := session.NewStore()
	if err != nil {
		return err
	}

	sessions, err := store.List()
	if err != nil {
		return err
	}

	if len(sessions) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no sessions")
		return nil
	}

	for _, sess := range sessions {
		var st claude.State
		if !kitty.TabExists(sess.KittyTabID) {
			st = claude.StateStopped
		} else if s, t, err := state.Read(sess.Name); err == nil && state.IsFresh(t) {
			st = mapStateString(s)
		} else {
			winID := sess.KittyWindowID
			if winID == 0 {
				id, err := kitty.FirstWindowInTab(sess.KittyTabID)
				if err == nil {
					winID = id
				}
			}
			if winID != 0 {
				text, err := kitty.GetText(winID)
				if err == nil {
					st = claude.DetectState(text)
				} else {
					st = claude.StateWorking
				}
			} else {
				st = claude.StateWorking
			}
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%-20s %-10s %s\n", sess.Name, st, sess.Dir)
	}
	return nil
}

// mapStateString converts a state file string to a claude.State.
func mapStateString(s string) claude.State {
	switch s {
	case "working":
		return claude.StateWorking
	case "idle":
		return claude.StateIdle
	case "input":
		return claude.StateNeedsInput
	case "waiting":
		return claude.StateWaiting
	default:
		return claude.StateUnknown
	}
}
