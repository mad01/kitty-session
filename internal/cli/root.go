package cli

import (
	"errors"
	"fmt"

	"github.com/mad01/kitty-session/internal/instance"
	"github.com/mad01/kitty-session/internal/launcher"
	"github.com/spf13/cobra"
)

var Version = "dev"

var rootCmd = &cobra.Command{
	Use:   "ks",
	Short: "Kitty Claude Session Manager",
	Long: `Kitty Claude Session Manager — keep Claude Code sessions as tabs in a kitty
instance that ks owns. Each tab pairs the ks sidebar with claude.

Run with no arguments to attach: start the instance if it is not running,
resume every active session whose claude window is gone, and focus the
session you used last. Use the subcommands for scripting and automation.

Sidebar keys (m opens the menu with the rest):
  j/k, 1-9    Move the cursor, jump to a row
  enter       Focus the agent under the cursor, reopening its tab if gone
  l / tab / q Hand the keyboard to this tab's claude window
  n           New agent (repo picker)
  r  c  d  u  Rename, close (keep), delete, restore
  /           Filter by name
  ?           Keys popup

From claude, your own kitty window keys bring you back to the sidebar
(cmd+] and cmd+[ in the dotfiles config); ks maps no chords of its own.`,
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
	printAttachResult(cmd, res)
	return nil
}

// printAttachResult reports one attach the way bare ks does: warnings and
// early exits on stderr, the counts on stdout. ks import shares it.
func printAttachResult(cmd *cobra.Command, res *launcher.AttachResult) {
	printWarnings(cmd, res.Warnings)
	for _, name := range res.Exited {
		fmt.Fprintf(cmd.ErrOrStderr(), "ks: %s exited right after launch\n", name)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "ks: %d resumed, %d already running, %d stopped\n",
		res.Resumed, res.Running, res.Stopped)
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

// resumeOnFreshStart brings every active session back when the command had
// to start the instance, before the command adds its own tab: the tabs then
// keep their order and the new session comes last. Problems are reported as
// warnings; the command's own work goes on.
func resumeOnFreshStart(cmd *cobra.Command, w wiring) {
	if !w.started {
		return
	}
	res, err := w.launcher.Resume()
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not resume sessions: %v\n", err)
		return
	}
	if res.Resumed+res.Running+len(res.Exited) > 0 {
		printAttachResult(cmd, res)
	}
}
