package cli

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"github.com/mad01/kitty-session/internal/kitty"
	"github.com/mad01/kitty-session/internal/repo/config"
	"github.com/mad01/kitty-session/internal/session"
	"github.com/mad01/kitty-session/internal/summary"
	"github.com/spf13/cobra"
)

var tmpSessionName string

var tmpCmd = &cobra.Command{
	Use:   "tmp",
	Short: "Create a temporary Claude session",
	Long:  "Create a named kitty tab with Claude in a temporary directory.",
	RunE:  runTmp,
}

func init() {
	tmpCmd.Flags().StringVarP(&tmpSessionName, "name", "n", "", "session name (auto-generated if omitted)")
	rootCmd.AddCommand(tmpCmd)
}

func runTmp(cmd *cobra.Command, args []string) error {
	store, err := session.NewStore()
	if err != nil {
		return err
	}

	cfg, _ := config.Load()

	tmpBase := cfg.EffectiveTmpDir()
	if tmpBase != "" {
		if err := os.MkdirAll(tmpBase, 0o755); err != nil {
			return fmt.Errorf("cannot create tmpdir: %w", err)
		}
	}
	tmpDir, err := os.MkdirTemp(tmpBase, "ks-*")
	if err != nil {
		return fmt.Errorf("cannot create temp directory: %w", err)
	}

	name := tmpSessionName
	if name == "" {
		name = fmt.Sprintf("tmp-%s", time.Now().Format("0102-1504"))
		if store.Exists(name) {
			b := make([]byte, 2)
			_, _ = rand.Read(b)
			name = name + "-" + hex.EncodeToString(b)
		}
	} else if store.Exists(name) {
		return fmt.Errorf("session %q already exists (use 'ks open %s' or 'ks close %s' first)", name, name, name)
	}

	layout := cfg.EffectiveLayout()

	windowID, err := kitty.LaunchTab(tmpDir, "--env", "KS_SESSION_NAME="+name, "--", "claude")
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

	sess := session.New(name, tmpDir, tabID, windowID)
	if layout == config.LayoutTab {
		shellWindowID, err := kitty.LaunchTabInWindow(windowID, tmpDir)
		if err != nil {
			return fmt.Errorf("cannot create shell tab: %w", err)
		}
		sess.KittyShellWindowID = shellWindowID
	} else {
		if err := kitty.LaunchSplit(tmpDir); err != nil {
			return fmt.Errorf("cannot create split: %w", err)
		}
	}

	if cfg.SummaryEnabled() {
		summaryWindowID, err := summary.LaunchTab(windowID, windowID, tmpDir)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not create summary tab: %v\n", err)
		} else {
			sess.KittySummaryWindowID = summaryWindowID
		}
	}

	if err := kitty.FocusWindow(windowID); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not focus claude pane: %v\n", err)
	}
	if err := store.Save(sess); err != nil {
		return fmt.Errorf("cannot save session: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "session %q created in %s\n", name, tmpDir)
	return nil
}
