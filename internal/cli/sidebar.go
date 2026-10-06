package cli

import (
	"fmt"
	"os"
	"strconv"

	"github.com/mad01/kitty-session/internal/instance"
	"github.com/mad01/kitty-session/internal/kitty"
	"github.com/mad01/kitty-session/internal/launcher"
	"github.com/mad01/kitty-session/internal/repo/config"
	"github.com/mad01/kitty-session/internal/session"
	"github.com/mad01/kitty-session/internal/sidebar"
	"github.com/spf13/cobra"
)

// Variables kitty exports into every window: the address it listens on
// (set to the ks socket, it marks a window inside the ks instance) and the
// id of the window the process runs in.
const (
	listenOnEnv = "KITTY_LISTEN_ON"
	windowIDEnv = "KITTY_WINDOW_ID"
)

var (
	sidebarSession   string
	sidebarSessionID string
)

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
		StringVar(&sidebarSessionID, "session-id", "", "the id of the session whose tab this sidebar sits in")
	sidebarCmd.Flags().
		StringVar(&sidebarSession, "session", "", "the name of the session whose tab this sidebar sits in")
	rootCmd.AddCommand(sidebarCmd)
}

func runSidebar(cmd *cobra.Command, args []string) error {
	store, cfg, err := storeAndConfig()
	if err != nil {
		return err
	}
	c, err := connectSidebar(cfg)
	if err != nil {
		return err
	}
	ownID, ownName := sidebarIdentity(store)
	agentRunning := false
	if agentFlag {
		if agent, err := startAgent(c.Socket()); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: agent failed to start: %v\n", err)
		} else {
			defer stopAgent(agent)
			stopAgentOnSignal(agent)
			agentRunning = true
		}
	}
	home := sidebarSessionID == "" && sidebarSession == "" // no session flag: the home tab
	if vars := homeVars(home, agentRunning); len(vars) > 0 {
		markHome(cmd, c, vars)
	}
	b, err := launcher.NewSidebarBackend(store, c, cfg, ownID)
	if err != nil {
		return err
	}
	return sidebar.Run(sidebar.Options{
		Session: ownName,
		Width:   cfg.EffectiveSidebarWidth(),
		Backend: b,
	})
}

// sidebarIdentity resolves which session's tab this sidebar sits in. The
// launcher passes --session-id (stable across renames); --session by name is
// kept for a human running the command. The id drives the own marker; the
// name is only for display, so an unknown name still shows the rest.
func sidebarIdentity(store *session.Store) (id, name string) {
	if sidebarSessionID != "" {
		if sess, err := store.FindByID(sidebarSessionID); err == nil {
			return sidebarSessionID, sess.Name
		}
		return sidebarSessionID, ""
	}
	if sidebarSession != "" {
		if sess, err := store.Load(sidebarSession); err == nil {
			return sess.ID, sess.Name
		}
		return "", sidebarSession
	}
	return "", ""
}

// homeVars returns the kitty user variables a sidebar sets on its own window.
// A session's sidebar sets none: the launcher tagged its window at launch.
// The home tab's sidebar sets HomeVar, which tells the launcher this tab is
// its to retire and recreate and not one the user opened; the instance's
// first window gets no --var at launch, so this is where it is tagged. With
// the agent running it adds HomeAgentVar, which keeps the home tab, and the
// agent with it, once session tabs exist.
func homeVars(home, agentRunning bool) []string {
	if !home {
		return nil
	}
	vars := []string{kitty.HomeVar + "=1"}
	if agentRunning {
		vars = append(vars, kitty.HomeAgentVar+"=1")
	}
	return vars
}

// markHome sets vars on the window this sidebar runs in. Only a sidebar
// inside the instance tags anything: run by hand in another kitty,
// KITTY_WINDOW_ID names one of that kitty's windows, and the same id in the
// instance may be any window at all.
func markHome(cmd *cobra.Command, c *kitty.Client, vars []string) {
	if !inInstance(c.Socket()) {
		return
	}
	id, err := strconv.Atoi(os.Getenv(windowIDEnv))
	if err != nil {
		return
	}
	if err := c.SetUserVars(id, vars...); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: cannot tag the home tab: %v\n", err)
	}
}

// inInstance reports whether this process runs in a window of the ks
// instance: kitty exports the address it listens on into every window.
func inInstance(socket string) bool {
	return os.Getenv(listenOnEnv) == socket
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
	if inInstance(socket) {
		return instance.Await(cfg)
	}
	return instance.Connect(cfg)
}
