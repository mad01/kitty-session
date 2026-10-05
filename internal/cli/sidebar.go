package cli

import (
	"fmt"

	"github.com/mad01/kitty-session/internal/tui"
	"github.com/spf13/cobra"
)

var sidebarSession string

var sidebarCmd = &cobra.Command{
	Use:   "sidebar",
	Short: "Run the session sidebar in the current window",
	Long: `Run the ks TUI in the current window. The instance starts one in its home
tab and one on the left of every session tab; run it by hand to get the TUI
anywhere. With --agent the background Haiku state monitor runs for as long
as the sidebar does.`,
	Args: cobra.NoArgs,
	RunE: runSidebar,
}

func init() {
	sidebarCmd.Flags().
		StringVar(&sidebarSession, "session", "", "the session whose tab this sidebar sits in")
	rootCmd.AddCommand(sidebarCmd)
}

func runSidebar(cmd *cobra.Command, args []string) error {
	if agentFlag {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		socket, err := cfg.Socket()
		if err != nil {
			return err
		}
		agent, err := startAgent(socket)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: agent failed to start: %v\n", err)
		} else {
			defer stopAgent(agent)
		}
	}
	return tui.Run(tui.Options{Session: sidebarSession})
}
