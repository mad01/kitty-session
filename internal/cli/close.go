package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var keepSession bool

var closeCmd = &cobra.Command{
	Use:   "close <name>",
	Short: "Close a session",
	Long:  "Close the session's tab and remove the session file. Use --keep to preserve the session for later recovery.",
	Args:  cobra.ExactArgs(1),
	RunE:  runClose,
}

func init() {
	closeCmd.Flags().BoolVar(&keepSession, "keep", false, "keep session file for later recovery")
	rootCmd.AddCommand(closeCmd)
}

func runClose(cmd *cobra.Command, args []string) error {
	name := args[0]

	w, err := offlineWiring()
	if err != nil {
		return err
	}
	sess, err := w.store.Load(name)
	if err != nil {
		return fmt.Errorf("session %q not found", name)
	}

	warnings, err := w.launcher.Close(sess, keepSession)
	printWarnings(cmd, warnings)
	if err != nil {
		return err
	}
	if keepSession {
		fmt.Fprintf(cmd.OutOrStdout(), "session %q tab closed (session kept for recovery)\n", name)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "session %q closed\n", name)
	return nil
}
