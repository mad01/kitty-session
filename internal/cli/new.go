package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mad01/kitty-session/internal/launcher"
	"github.com/mad01/kitty-session/internal/repo/config"
	"github.com/mad01/kitty-session/internal/session"
	"github.com/spf13/cobra"
)

var (
	newName string
	newDir  string
)

var newCmd = &cobra.Command{
	Use:   "new",
	Short: "Create a new kitty session",
	Long:  "Create a named kitty tab with Claude on top and a shell on bottom.",
	RunE:  runNew,
}

func init() {
	newCmd.Flags().StringVarP(&newName, "name", "n", "", "session name (required)")
	newCmd.Flags().StringVarP(&newDir, "dir", "d", "", "working directory (default: cwd)")
	_ = newCmd.MarkFlagRequired("name")
	rootCmd.AddCommand(newCmd)
}

func runNew(cmd *cobra.Command, args []string) error {
	store, err := session.NewStore()
	if err != nil {
		return err
	}

	dir := newDir
	if dir == "" {
		dir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("cannot determine working directory: %w", err)
		}
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("cannot resolve directory: %w", err)
	}

	cfg, _ := config.Load()
	res, err := launcher.Open(store, cfg, launcher.Request{Name: newName, Dir: dir})
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
