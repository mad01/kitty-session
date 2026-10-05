package cli

import (
	"errors"
	"fmt"

	"github.com/mad01/kitty-session/internal/instance"
	"github.com/spf13/cobra"
)

var Version = "dev"

var rootCmd = &cobra.Command{
	Use:   "ks",
	Short: "Kitty Claude Session Manager",
	Long: `Kitty Claude Session Manager — keep Claude Code sessions as tabs in a kitty
instance that ks owns. Each tab pairs a sidebar (the ks TUI) with claude.

Run with no arguments to attach: start the instance if it is not running,
resume every active session whose claude window is gone, and focus the
session you used last. Use the subcommands for scripting and automation.

Sidebar keybindings (press ? in the sidebar for full help):
  j/k         Navigate sessions
  o / enter   Open or focus session
  n           Create new session
  r           Rename session
  c           Close tab (keep session)
  d           Delete session
  /           Fuzzy search
  ?           Toggle help
  q           Quit the sidebar`,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          runAttach,
}

func Execute() error {
	return rootCmd.Execute()
}

// runAttach is bare `ks`: make sure the instance is up, bring every active
// session back, and focus the most recently used one.
func runAttach(cmd *cobra.Command, args []string) error {
	if agentFlag {
		noteAgentOnRunningInstance(cmd)
	}
	w, err := ensureWiring(agentFlag)
	if err != nil {
		return err
	}
	res, err := w.launcher.Attach()
	if err != nil {
		return err
	}
	printWarnings(cmd, res.Warnings)
	fmt.Fprintf(cmd.OutOrStdout(), "ks: %d resumed, %d already running, %d stopped\n",
		res.Resumed, res.Running, res.Stopped)
	return nil
}

// noteAgentOnRunningInstance tells the user when --agent cannot take effect
// because the instance, whose home sidebar would host the agent, is already up.
func noteAgentOnRunningInstance(cmd *cobra.Command) {
	cfg, err := loadConfig()
	if err != nil {
		return // ensureWiring reports it
	}
	if _, err := instance.Connect(cfg); err == nil {
		fmt.Fprintln(cmd.ErrOrStderr(),
			"note: ks instance already running; --agent takes effect when the instance starts")
	} else if !errors.Is(err, instance.ErrNotRunning) {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: %v\n", err)
	}
}
