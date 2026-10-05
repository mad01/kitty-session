package cli

import (
	"fmt"

	"github.com/mad01/kitty-session/internal/claude"
	"github.com/mad01/kitty-session/internal/instance"
	"github.com/mad01/kitty-session/internal/kitty"
	"github.com/mad01/kitty-session/internal/launcher"
	"github.com/mad01/kitty-session/internal/session"
	"github.com/mad01/kitty-session/internal/state"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all sessions",
	Long:  "Show all sessions with their state. Without a running ks instance every session is stopped.",
	RunE:  runList,
}

func init() {
	rootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, args []string) error {
	w, err := offlineWiring()
	if err != nil {
		return err
	}
	sessions, err := w.store.List()
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no sessions")
		return nil
	}

	c, err := instance.Client(w.cfg)
	if err != nil {
		return err
	}
	down := c.Ping() != nil
	for _, sess := range sessions {
		st := claude.StateStopped
		if !down {
			st = listState(w.launcher, c, sess)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%-20s %-10s %s\n", sess.Name, st, sess.Dir)
	}
	if down {
		fmt.Fprintln(cmd.OutOrStdout(), "ks instance not running")
	}
	return nil
}

// listState resolves a session's state: the record's own status first, then
// whether its claude window is in the instance, then a fresh state file, then
// the terminal text.
func listState(l *launcher.Launcher, c *kitty.Client, sess *session.Session) claude.State {
	if !sess.IsActive() || !l.Alive(sess) {
		return claude.StateStopped
	}
	if s, t, err := state.Read(sess.Name); err == nil && state.IsFresh(t) {
		return claude.ParseState(s)
	}
	text, err := c.GetText(sess.KittyWindowID)
	if err != nil {
		return claude.StateWorking
	}
	return claude.DetectState(text)
}
