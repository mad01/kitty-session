package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var renameCmd = &cobra.Command{
	Use:   "rename [<old-name>] <new-name>",
	Short: "Rename a session",
	Long: `Rename the session record and its state file, and retitle its tab when it
is open.

With one argument the session this command runs in is renamed: the launcher
exports KS_SESSION_ID into every session's windows, so a Claude Code session
in a ks tab can rename itself with "ks rename <new-name>".`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runRename,
}

func init() {
	rootCmd.AddCommand(renameCmd)
}

func runRename(cmd *cobra.Command, args []string) error {
	w, err := offlineWiring()
	if err != nil {
		return err
	}
	newName := args[len(args)-1]
	oldName, err := nameOrSelf(w.store, args[:len(args)-1])
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
