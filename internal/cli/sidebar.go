package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/mad01/kitty-session/internal/instance"
	"github.com/mad01/kitty-session/internal/kitty"
	"github.com/mad01/kitty-session/internal/launcher"
	"github.com/mad01/kitty-session/internal/repo/config"
	"github.com/mad01/kitty-session/internal/sidebar"
	"github.com/spf13/cobra"
)

// listenOnEnv is the variable kitty exports into every window with the
// address it listens on; set to the ks socket it marks a window inside the
// ks instance.
const listenOnEnv = "KITTY_LISTEN_ON"

var sidebarSession string

var sidebarCmd = &cobra.Command{
	Use:   "sidebar",
	Short: "Run the agent sidebar in the current window",
	Long: `Run the ks sidebar in the current window. The instance starts one in its home
tab and one on the left of every session tab; run it by hand to get the
sidebar in any terminal. It needs the instance to be running and exits with
"ks instance not running" otherwise. With --agent the background Haiku state
monitor runs for as long as the sidebar does.`,
	Args: cobra.NoArgs,
	RunE: runSidebar,
}

func init() {
	sidebarCmd.Flags().
		StringVar(&sidebarSession, "session", "", "the session whose tab this sidebar sits in")
	rootCmd.AddCommand(sidebarCmd)
}

func runSidebar(cmd *cobra.Command, args []string) error {
	store, cfg, err := storeAndConfig()
	if err != nil {
		return err
	}
	c, err := connectSidebar(cfg)
	if errors.Is(err, instance.ErrNotRunning) {
		return instance.ErrNotRunning
	}
	if err != nil {
		return err
	}
	if agentFlag {
		agent, err := startAgent(c.Socket())
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: agent failed to start: %v\n", err)
		} else {
			defer stopAgent(agent)
		}
	}
	b, err := launcher.NewSidebarBackend(store, c, cfg, sidebarSession)
	if err != nil {
		return err
	}
	return sidebar.Run(sidebar.Options{
		Session: sidebarSession,
		Width:   cfg.EffectiveSidebarWidth(),
		Backend: b,
	})
}

// connectSidebar returns a client for the running instance. Inside the
// instance it waits for the socket to answer: kitty runs the home sidebar as
// its first window, before the remote-control socket necessarily does.
// Anywhere else a silent instance is reported at once.
func connectSidebar(cfg *config.Config) (*kitty.Client, error) {
	socket, err := cfg.Socket()
	if err != nil {
		return nil, err
	}
	if os.Getenv(listenOnEnv) == socket {
		return instance.Await(cfg)
	}
	return instance.Connect(cfg)
}
