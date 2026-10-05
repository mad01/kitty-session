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
		fmt.Fprintf(cmd.OutOrStdout(), "%-20s %-10s %s\n", sess.Name, listState(sess), sess.Dir)
	}
	return nil
}

// listState resolves a session's state: the record's own status first, then
// the kitty tab, then a fresh state file, then the terminal text.
func listState(sess *session.Session) claude.State {
	if !sess.IsActive() || !kitty.TabExists(sess.KittyTabID) {
		return claude.StateStopped
	}
	if s, t, err := state.Read(sess.Name); err == nil && state.IsFresh(t) {
		return claude.ParseState(s)
	}
	winID := sess.KittyWindowID
	if winID == 0 {
		id, err := kitty.FirstWindowInTab(sess.KittyTabID)
		if err != nil {
			return claude.StateWorking
		}
		winID = id
	}
	text, err := kitty.GetText(winID)
	if err != nil {
		return claude.StateWorking
	}
	return claude.DetectState(text)
}
