package cli

import (
	"fmt"
	"io"

	"github.com/mad01/kitty-session/internal/hooks"
	"github.com/spf13/cobra"
)

var hooksCmd = &cobra.Command{
	Use:   "hooks",
	Short: "Claude Code hook entries from older ks versions",
	Long: `The Claude Code hooks ks needs for state detection are injected into every
claude it launches (with --settings), so nothing has to be written to
~/.claude/settings.json. Older ks versions registered them there; these
commands find and remove what they left behind.`,
}

var hooksInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Nothing to install: ks hooks are injected at launch",
	Long: `ks hooks are injected at launch, so there is nothing to install. Entries an
older ks wrote to ~/.claude/settings.json are removed, as ks hooks uninstall
does.`,
	RunE: runHooksInstall,
}

var hooksUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove the ks hook entries an older ks wrote to Claude Code settings",
	RunE:  runHooksUninstall,
}

var hooksStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show how the ks hooks reach a session and any leftover entries",
	RunE:  runHooksStatus,
}

func init() {
	hooksCmd.AddCommand(hooksInstallCmd)
	hooksCmd.AddCommand(hooksUninstallCmd)
	hooksCmd.AddCommand(hooksStatusCmd)
	rootCmd.AddCommand(hooksCmd)
}

// inspectHooks reads the Claude Code settings file once for whichever
// subcommand runs.
func inspectHooks() (hooks.Status, error) {
	path, err := hooks.Path()
	if err != nil {
		return hooks.Status{}, err
	}
	return hooks.Inspect(path)
}

// removeLeftovers rewrites the settings file without its ks groups and says
// so, only when it holds any: a file with nothing to remove is not touched,
// so a settings manager that watches it sees no spurious rewrite.
func removeLeftovers(out io.Writer, st hooks.Status) error {
	if len(st.Leftovers) == 0 {
		return nil
	}
	if err := hooks.Uninstall(st.Path); err != nil {
		return err
	}
	fmt.Fprintf(out, "removed leftover ks hooks from %s\n", st.Path)
	return nil
}

// runHooksInstall says so when there is nothing to install and cleans up
// the entries an older ks left in the settings file.
func runHooksInstall(cmd *cobra.Command, args []string) error {
	st, err := inspectHooks()
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "ks hooks are injected at launch; nothing to install")
	return removeLeftovers(out, st)
}

func runHooksUninstall(cmd *cobra.Command, args []string) error {
	st, err := inspectHooks()
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if len(st.Leftovers) == 0 {
		fmt.Fprintf(out, "no leftover ks hooks in %s\n", st.Path)
		return nil
	}
	return removeLeftovers(out, st)
}

func runHooksStatus(cmd *cobra.Command, args []string) error {
	st, err := inspectHooks()
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), st.Full())
	return nil
}
