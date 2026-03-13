package cli

import (
	"fmt"

	"github.com/mad01/kitty-session/internal/kitty"
	"github.com/mad01/kitty-session/internal/session"
	"github.com/spf13/cobra"
)

var keepSession bool

var closeCmd = &cobra.Command{
	Use:   "close <name>",
	Short: "Close a session",
	Long:  "Close the kitty tab and remove the session file. Use --keep to preserve the session for later recovery.",
	Args:  cobra.ExactArgs(1),
	RunE:  runClose,
}

func init() {
	closeCmd.Flags().BoolVar(&keepSession, "keep", false, "keep session file for later recovery")
	rootCmd.AddCommand(closeCmd)
}

func runClose(cmd *cobra.Command, args []string) error {
	name := args[0]

	store, err := session.NewStore()
	if err != nil {
		return err
	}

	sess, err := store.Load(name)
	if err != nil {
		return fmt.Errorf("session %q not found", name)
	}

	// Close the kitty tab(s) if still running
	if kitty.TabExists(sess.KittyTabID) {
		_ = kitty.CloseTab(sess.KittyTabID)
	}
	if sess.KittyShellWindowID != 0 {
		_ = kitty.CloseTabForWindow(sess.KittyShellWindowID)
	}
	if sess.KittySummaryWindowID != 0 {
		_ = kitty.CloseTabForWindow(sess.KittySummaryWindowID)
	}

	if keepSession {
		fmt.Fprintf(cmd.OutOrStdout(), "session %q tab closed (session kept for recovery)\n", name)
		return nil
	}

	if err := store.Delete(name); err != nil {
		return fmt.Errorf("cannot delete session file: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "session %q closed\n", name)
	return nil
}
