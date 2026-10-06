package cli

import (
	"fmt"
	"strconv"

	"github.com/mad01/kitty-session/internal/launcher"
	"github.com/spf13/cobra"
)

var moveCmd = &cobra.Command{
	Use:   "move [<name>] <where>",
	Short: "Move a session's tab in the sidebar order",
	Long: `Move the session's tab to another place in the sidebar's agent list, which
is the instance's tab order (cmd+N). <where> is top, bottom, up, down or a
position counted from 1; a position past either end means that end.

With one argument the session this command runs in moves: the launcher exports
KS_SESSION_ID into every session's windows, so a Claude Code session in a ks
tab can reorder itself with "ks move <where>". Only a session with an open tab
can move. The order is written to the session records and comes back on the
next attach. The instance must be running.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runMove,
}

func init() {
	rootCmd.AddCommand(moveCmd)
}

func runMove(cmd *cobra.Command, args []string) error {
	where, err := parseMoveTarget(args[len(args)-1])
	if err != nil {
		return err
	}
	w, err := offlineWiring()
	if err != nil {
		return err
	}
	name, err := nameOrSelf(w.store, args[:len(args)-1])
	if err != nil {
		return err
	}
	res, err := w.launcher.Move(name, where)
	if err != nil {
		return err
	}
	printWarnings(cmd, res.Warnings)
	if res.From == res.To {
		fmt.Fprintf(cmd.OutOrStdout(), "session %q already at %d\n", name, res.To)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "session %q moved from %d to %d\n", name, res.From, res.To)
	return nil
}

// parseMoveTarget reads the <where> argument: top, bottom, up, down or a
// position counted from 1.
func parseMoveTarget(s string) (launcher.MoveTarget, error) {
	switch s {
	case "top":
		return launcher.MoveTarget{Kind: launcher.MoveTop}, nil
	case "bottom":
		return launcher.MoveTarget{Kind: launcher.MoveBottom}, nil
	case "up":
		return launcher.MoveTarget{Kind: launcher.MoveUp}, nil
	case "down":
		return launcher.MoveTarget{Kind: launcher.MoveDown}, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return launcher.MoveTarget{}, fmt.Errorf(
			"invalid destination %q: want top, bottom, up, down or a position from 1", s)
	}
	return launcher.MoveTarget{Kind: launcher.MoveTo, Position: n}, nil
}
