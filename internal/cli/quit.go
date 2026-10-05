package cli

import (
	"errors"
	"fmt"

	"github.com/mad01/kitty-session/internal/instance"
	"github.com/spf13/cobra"
)

var quitCmd = &cobra.Command{
	Use:   "quit",
	Short: "Close the ks kitty instance",
	Long: `Close every tab of the ks instance, which ends it. Session records stay
active, so the next attach (bare ks) brings them all back.`,
	Args: cobra.NoArgs,
	RunE: runQuit,
}

func init() {
	rootCmd.AddCommand(quitCmd)
}

func runQuit(cmd *cobra.Command, args []string) error {
	store, cfg, err := storeAndConfig()
	if err != nil {
		return err
	}
	c, err := instance.Connect(cfg)
	if errors.Is(err, instance.ErrNotRunning) {
		fmt.Fprintln(cmd.OutOrStdout(), "ks instance not running")
		return nil
	}
	if err != nil {
		return err
	}
	sessions, err := store.List()
	if err != nil {
		return err
	}
	active := 0
	for _, s := range sessions {
		if s.IsActive() {
			active++
		}
	}
	if err := instance.Shutdown(c); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(),
		"ks instance closed; %d active session(s) will resume on the next attach\n", active)
	return nil
}
