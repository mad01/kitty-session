package cli

import (
	"fmt"

	"github.com/mad01/kitty-session/internal/claude"
	"github.com/mad01/kitty-session/internal/kitty"
	"github.com/mad01/kitty-session/internal/session"
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
		var state claude.State
		if !kitty.TabExists(sess.KittyTabID) {
			state = claude.StateStopped
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
					state = claude.DetectState(text)
				} else {
					state = claude.StateWorking
				}
			} else {
				state = claude.StateWorking
			}
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%-20s %-10s %s\n", sess.Name, state, sess.Dir)
	}
	return nil
}
