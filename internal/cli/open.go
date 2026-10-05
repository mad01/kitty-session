package cli

import (
	"fmt"

	"github.com/mad01/kitty-session/internal/launcher"
	"github.com/spf13/cobra"
)

var openCmd = &cobra.Command{
	Use:   "open <name>",
	Short: "Focus or recreate a session",
	Long:  "Focus a running session or bring a stopped one back in the ks instance.",
	Args:  cobra.ExactArgs(1),
	RunE:  runOpen,
}

func init() {
	rootCmd.AddCommand(openCmd)
}

func runOpen(cmd *cobra.Command, args []string) error {
	name := args[0]

	w, err := ensureWiring(false)
	if err != nil {
		return err
	}
	if !w.store.Exists(name) {
		return fmt.Errorf("session %q not found", name)
	}
	res, err := w.launcher.Open(launcher.Request{Name: name, Resume: launcher.ResumeStored})
	if err != nil {
		return err
	}
	printWarnings(cmd, res.Warnings)

	if res.Focused {
		fmt.Fprintf(cmd.OutOrStdout(), "session %q focused\n", name)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "session %q recreated in %s\n", name, res.Session.Dir)
	return nil
}
