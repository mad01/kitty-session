package cli

import (
	"fmt"
	"os"

	"github.com/mad01/kitty-session/internal/hooks"
	"github.com/spf13/cobra"
)

var hooksCmd = &cobra.Command{
	Use:   "hooks",
	Short: "Manage Claude Code hooks for state detection",
}

var hooksInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install ks hooks into Claude Code settings",
	RunE:  runHooksInstall,
}

var hooksUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove ks hooks from Claude Code settings",
	RunE:  runHooksUninstall,
}

func init() {
	hooksCmd.AddCommand(hooksInstallCmd)
	hooksCmd.AddCommand(hooksUninstallCmd)
	rootCmd.AddCommand(hooksCmd)
}

// hooksTarget returns the settings file to edit and the ks binary to register.
func hooksTarget() (path, binary string, err error) {
	binary, err = os.Executable()
	if err != nil {
		return "", "", fmt.Errorf("cannot determine ks binary path: %w", err)
	}
	path, err = hooks.Path()
	if err != nil {
		return "", "", err
	}
	return path, binary, nil
}

func runHooksInstall(cmd *cobra.Command, args []string) error {
	path, binary, err := hooksTarget()
	if err != nil {
		return err
	}
	if err := hooks.Install(path, binary); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "hooks installed")
	return nil
}

func runHooksUninstall(cmd *cobra.Command, args []string) error {
	path, binary, err := hooksTarget()
	if err != nil {
		return err
	}
	if err := hooks.Uninstall(path, binary); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "hooks uninstalled")
	return nil
}
