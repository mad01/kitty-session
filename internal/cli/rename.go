package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var renameCmd = &cobra.Command{
	Use:   "rename <old-name> <new-name>",
	Short: "Rename a session",
	Long:  "Rename the session record and its state file, and retitle its tab when it is open.",
	Args:  cobra.ExactArgs(2),
	RunE:  runRename,
}

func init() {
	rootCmd.AddCommand(renameCmd)
}

func runRename(cmd *cobra.Command, args []string) error {
	oldName, newName := args[0], args[1]

	w, err := offlineWiring()
	if err != nil {
		return err
	}
	_, warnings, err := w.launcher.Rename(oldName, newName)
	if err != nil {
		return err
	}
	printWarnings(cmd, warnings)

	fmt.Fprintf(cmd.OutOrStdout(), "session %q renamed to %q\n", oldName, newName)
	return nil
}
