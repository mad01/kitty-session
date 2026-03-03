package cli

import (
	"fmt"

	"github.com/mad01/kitty-session/internal/kitty"
	"github.com/mad01/kitty-session/internal/session"
	"github.com/mad01/kitty-session/internal/state"
	"github.com/spf13/cobra"
)

var renameCmd = &cobra.Command{
	Use:   "rename <old-name> <new-name>",
	Short: "Rename a session",
	Args:  cobra.ExactArgs(2),
	RunE:  runRename,
}

func init() {
	rootCmd.AddCommand(renameCmd)
}

func runRename(cmd *cobra.Command, args []string) error {
	oldName, newName := args[0], args[1]

	store, err := session.NewStore()
	if err != nil {
		return err
	}

	sess, err := store.Rename(oldName, newName)
	if err != nil {
		return err
	}

	state.Rename(oldName, newName)

	if kitty.TabExists(sess.KittyTabID) {
		_ = kitty.FocusTab(sess.KittyTabID)
		_ = kitty.SetTabTitle(newName)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "session %q renamed to %q\n", oldName, newName)
	return nil
}
