package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mad01/kitty-session/internal/launcher"
	"github.com/spf13/cobra"
)

var (
	newName string
	newDir  string
)

var newCmd = &cobra.Command{
	Use:   "new",
	Short: "Create a new session",
	Long:  "Create a session tab in the ks instance: a sidebar on the left, claude on the right.",
	RunE:  runNew,
}

func init() {
	newCmd.Flags().StringVarP(&newName, "name", "n", "", "session name (required)")
	newCmd.Flags().StringVarP(&newDir, "dir", "d", "", "working directory (default: cwd)")
	_ = newCmd.MarkFlagRequired("name")
	rootCmd.AddCommand(newCmd)
}

func runNew(cmd *cobra.Command, args []string) error {
	dir := newDir
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("cannot determine working directory: %w", err)
		}
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("cannot resolve directory: %w", err)
	}

	w, err := ensureWiring(false)
	if err != nil {
		return err
	}
	res, err := w.launcher.Open(launcher.Request{Name: newName, Dir: dir})
	if err != nil {
		return withExistsHint(err, newName)
	}
	printWarnings(cmd, res.Warnings)

	fmt.Fprintf(cmd.OutOrStdout(), "session %q created in %s\n", newName, dir)
	return nil
}

// withExistsHint tells the user what to do about a taken name; other errors
// pass through.
func withExistsHint(err error, name string) error {
	if !errors.Is(err, launcher.ErrExists) {
		return err
	}
	return fmt.Errorf("%w (use 'ks open %s' or 'ks close %s' first)", err, name, name)
}

// printWarnings reports the launcher's non-fatal problems on stderr.
func printWarnings(cmd *cobra.Command, warnings []error) {
	for _, w := range warnings {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: %v\n", w)
	}
}
