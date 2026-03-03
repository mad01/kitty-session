package cli

import (
	"fmt"
	"os"

	"github.com/mad01/kitty-session/internal/tui"
	"github.com/spf13/cobra"
)

var Version = "dev"

var rootCmd = &cobra.Command{
	Use:   "ks",
	Short: "Kitty Claude Session Manager",
	Long: `Kitty Claude Session Manager — manage named kitty sessions with Claude on top
and a shell on bottom.

Run with no arguments to launch the interactive TUI. Use subcommands
for scripting and automation.

TUI keybindings (press ? in the TUI for full help):
  j/k         Navigate sessions
  o / enter   Open or focus session
  n           Create new session
  c           Close tab (keep session)
  d           Delete session
  /           Fuzzy search
  ?           Toggle help
  q           Quit`,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if agentFlag {
			agent, err := startAgent()
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: agent failed to start: %v\n", err)
			} else {
				defer stopAgent(agent)
			}
		}
		return tui.Run()
	},
}

func Execute() error {
	return rootCmd.Execute()
}
