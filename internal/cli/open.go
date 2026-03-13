package cli

import (
	"fmt"

	"github.com/mad01/kitty-session/internal/kitty"
	"github.com/mad01/kitty-session/internal/repo/config"
	"github.com/mad01/kitty-session/internal/session"
	"github.com/mad01/kitty-session/internal/summary"
	"github.com/spf13/cobra"
)

var openCmd = &cobra.Command{
	Use:   "open <name>",
	Short: "Focus or recreate a session",
	Long:  "Focus a running session or recreate a stopped one.",
	Args:  cobra.ExactArgs(1),
	RunE:  runOpen,
}

func init() {
	rootCmd.AddCommand(openCmd)
}

func runOpen(cmd *cobra.Command, args []string) error {
	name := args[0]

	store, err := session.NewStore()
	if err != nil {
		return err
	}

	sess, err := store.Load(name)
	if err != nil {
		return fmt.Errorf("session %q not found", name)
	}

	// If the tab is still alive, focus the Claude pane directly
	if kitty.TabExists(sess.KittyTabID) {
		if sess.KittyWindowID != 0 {
			if err := kitty.FocusWindow(sess.KittyWindowID); err != nil {
				return fmt.Errorf("cannot focus window: %w", err)
			}
		} else {
			if err := kitty.FocusTab(sess.KittyTabID); err != nil {
				return fmt.Errorf("cannot focus tab: %w", err)
			}
		}
		fmt.Fprintf(cmd.OutOrStdout(), "session %q focused\n", name)
		return nil
	}

	// Tab is gone — recreate the session
	cfg, _ := config.Load()
	layout := cfg.EffectiveLayout()

	windowID, err := kitty.LaunchTab(sess.Dir, "--env", "KS_SESSION_NAME="+name, "--", "claude")
	if err != nil {
		return fmt.Errorf("cannot create tab: %w", err)
	}

	if err := kitty.SetTabTitle(name); err != nil {
		return fmt.Errorf("cannot set tab title: %w", err)
	}

	tabID, err := kitty.FindTabForWindow(windowID)
	if err != nil {
		return fmt.Errorf("cannot find tab: %w", err)
	}

	sess.KittyShellWindowID = 0
	sess.KittySummaryWindowID = 0
	if layout == config.LayoutTab {
		shellWindowID, err := kitty.LaunchTabInWindow(windowID, sess.Dir)
		if err != nil {
			return fmt.Errorf("cannot create shell tab: %w", err)
		}
		sess.KittyShellWindowID = shellWindowID
	} else {
		if err := kitty.LaunchSplit(sess.Dir); err != nil {
			return fmt.Errorf("cannot create split: %w", err)
		}
	}

	// Launch summary tab if enabled
	if cfg.SummaryEnabled() {
		summaryWindowID, err := summary.LaunchTab(windowID, windowID, sess.Dir)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not create summary tab: %v\n", err)
		} else {
			sess.KittySummaryWindowID = summaryWindowID
		}
	}

	if err := kitty.FocusWindow(windowID); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not focus claude pane: %v\n", err)
	}

	// Update session with new tab and window IDs
	sess.KittyTabID = tabID
	sess.KittyWindowID = windowID
	if err := store.Save(sess); err != nil {
		return fmt.Errorf("cannot save session: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "session %q recreated in %s\n", name, sess.Dir)
	return nil
}
