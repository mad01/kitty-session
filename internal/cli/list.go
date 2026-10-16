package cli

import (
	"fmt"
	"time"

	"github.com/mad01/kitty-session/internal/claude"
	"github.com/mad01/kitty-session/internal/kitty"
	"github.com/mad01/kitty-session/internal/launcher"
	"github.com/mad01/kitty-session/internal/session"
	"github.com/mad01/kitty-session/internal/sidebar"
	"github.com/mad01/kitty-session/internal/state"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all sessions",
	Long:  "Show all sessions with their state. Without a running ks instance every session is stopped.",
	RunE:  runList,
}

func init() {
	rootCmd.AddCommand(listCmd)
}

// readState reads a session's state file; tests substitute a fake.
var readState = state.Read

// paneText reads a window's terminal text with its scrollback, so the last
// lines are the live screen even when the user has scrolled the window up;
// kindState cuts it to the window's rows. Tests substitute a fake.
var paneText = (*kitty.Client).GetTextAll

// stoppedLabel is what the state column prints for a session without a live
// agent window, in the vocabulary the sidebar shares.
var stoppedLabel = sidebar.StateStopped.String()

func runList(cmd *cobra.Command, args []string) error {
	w, err := offlineWiring()
	if err != nil {
		return err
	}
	sessions, err := w.store.List()
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no sessions")
		return nil
	}

	// One snapshot of the instance serves every session; a failure means the
	// instance is down, so every active session reads as stopped.
	all, lsErr := w.kitty.Windows()
	down := lsErr != nil
	for _, sess := range sessions {
		st := stoppedLabel
		if !down {
			st = listState(w.kitty, all, sess)
		}
		fmt.Fprintf(
			cmd.OutOrStdout(),
			"%-20s %-7s %-10s %s\n",
			sess.Name,
			sess.Kind(),
			st,
			sess.Dir,
		)
	}
	if down {
		fmt.Fprintln(cmd.OutOrStdout(), "ks instance not running")
	}
	return nil
}

// listState resolves the state label ks list prints for a session from one
// instance snapshot, in the sidebar's order: the record's own status first,
// then whether its agent window is in the snapshot, then the window's
// Program Status Protocol record when kitty lists one and no fresh state
// file outranks it (launcher.ReportedState, shared with the sidebar), and
// the kind's own rules last, in kindState.
func listState(c *kitty.Client, all []kitty.Window, sess *session.Session) string {
	if !sess.IsActive() {
		return stoppedLabel
	}
	w, ok := launcher.ClaudeWindow(all, sess)
	if !ok {
		return stoppedLabel
	}
	file, fileAt, err := readState(sess.Name)
	if err != nil { // no state file
		file, fileAt = "", time.Time{}
	}
	if st, ok := launcher.ReportedState(w.Status, fileAt, w.TabActive); ok {
		return st.String()
	}
	return kindState(c, w, sess, file, fileAt).String()
}

// kindState is listState by the kind's own rules: without a Program Status
// record, or with a fresh state file. file and fileAt are the state file's
// state and stamp, empty and zero without one. A shell is idle, since
// nothing else reports its state. A pi session reads its state file,
// whatever its age, and is idle without one. A claude session reads a fresh
// state file, then the terminal text of its window, where an error file of
// any age still wins unless the text says working: a failed turn stays
// visible until the user retries, as it does in the sidebar.
func kindState(
	c *kitty.Client, w kitty.Window, sess *session.Session, file string, fileAt time.Time,
) claude.State {
	switch sess.Kind() {
	case session.AgentShell:
		return claude.StateIdle
	case session.AgentPi:
		if file != "" {
			return claude.ParseState(file)
		}
		return claude.StateIdle
	}
	if state.IsFresh(fileAt) {
		return claude.ParseState(file)
	}
	text, err := paneText(c, w.ID)
	if err != nil {
		return claude.StateWorking
	}
	st := claude.DetectState(claude.LiveScreen(text, w.Lines))
	if st != claude.StateWorking && claude.ParseState(file) == claude.StateError {
		return claude.StateError
	}
	return st
}
