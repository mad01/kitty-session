package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mad01/kitty-session/internal/kitty"
	"github.com/mad01/kitty-session/internal/session"
	"github.com/spf13/cobra"
)

var newName string
var newDir string

var newCmd = &cobra.Command{
	Use:   "new",
	Short: "Create a new kitty session",
	Long:  "Create a named kitty tab with Claude on top and a shell on bottom.",
	RunE:  runNew,
}

func init() {
	newCmd.Flags().StringVarP(&newName, "name", "n", "", "session name (required)")
	newCmd.Flags().StringVarP(&newDir, "dir", "d", "", "working directory (default: cwd)")
	_ = newCmd.MarkFlagRequired("name")
	rootCmd.AddCommand(newCmd)
}

func runNew(cmd *cobra.Command, args []string) error {
	store, err := session.NewStore()
	if err != nil {
		return err
	}

	if store.Exists(newName) {
		return fmt.Errorf("session %q already exists (use 'ks open %s' or 'ks close %s' first)", newName, newName, newName)
	}

	dir := newDir
	if dir == "" {
		dir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("cannot determine working directory: %w", err)
		}
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("cannot resolve directory: %w", err)
	}

	// Launch tab with claude
	windowID, err := kitty.LaunchTab(dir, "--", "claude")
	if err != nil {
		return fmt.Errorf("cannot create tab: %w", err)
	}

	// Set tab title to session name
	if err := kitty.SetTabTitle(newName); err != nil {
		return fmt.Errorf("cannot set tab title: %w", err)
	}

	// Get the tab ID from the window we just created
	tabID, err := kitty.FindTabForWindow(windowID)
	if err != nil {
		return fmt.Errorf("cannot find tab: %w", err)
	}

	// Launch shell split below
	if err := kitty.LaunchSplit(dir); err != nil {
		return fmt.Errorf("cannot create split: %w", err)
	}

	// Focus back on the claude window (top pane)
	if err := kitty.FocusWindow(windowID); err != nil {
		// Non-fatal: session is still usable
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not focus claude pane: %v\n", err)
	}

	sess := session.New(newName, dir, tabID)
	if err := store.Save(sess); err != nil {
		return fmt.Errorf("cannot save session: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "session %q created in %s\n", newName, dir)
	return nil
}
