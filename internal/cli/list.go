package cli

import (
	"fmt"

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
		status := "stopped"
		if kitty.TabExists(sess.KittyTabID) {
			status = "running"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%-20s %-10s %s\n", sess.Name, status, sess.Dir)
	}
	return nil
}
