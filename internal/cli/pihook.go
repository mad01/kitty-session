package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/mad01/kitty-session/internal/session"
	"github.com/mad01/kitty-session/internal/state"
	"github.com/spf13/cobra"
)

// piHookPayload is the JSON object the ks pi extension (pi/ks-agent-state.ts)
// pipes to _pi-hook on stdin. event is one of the names piStateForEvent maps
// or session_shutdown; session_id and session_file are pi's own, read from
// its session manager when the extension could; reason is pi's reason for a
// session_shutdown (quit, reload, new, resume, fork) and empty otherwise.
type piHookPayload struct {
	Event       string `json:"event"`
	SessionID   string `json:"session_id"`
	SessionFile string `json:"session_file"`
	Reason      string `json:"reason"`
}

var piHookCmd = &cobra.Command{
	Use:    "_pi-hook",
	Short:  "Handle pi extension events",
	Hidden: true,
	RunE:   runPiHook,
}

func init() {
	rootCmd.AddCommand(piHookCmd)
}

// runPiHook handles one event from the ks pi extension, the pi counterpart of
// runHook: the state file is its job, and a record problem goes to stderr
// while the command still exits 0, so pi never sees a failed hook. There is
// no process-tree guard like firedByTopLevelClaude: the extension reports
// only from pi's own TUI (ctx.mode === "tui"), which a nested pi -p run
// inside the session, the case the claude guard exists for, never is.
func runPiHook(cmd *cobra.Command, args []string) error {
	id, name := os.Getenv("KS_SESSION_ID"), os.Getenv("KS_SESSION_NAME")
	if id == "" && name == "" {
		return nil // not inside a ks session, nothing to do
	}
	payload, err := readHookPayload[piHookPayload](cmd.InOrStdin())
	if err != nil {
		return err
	}
	isShutdown := payload.Event == "session_shutdown"
	s := piStateForEvent(payload.Event)
	if !isShutdown && s == "" {
		return nil
	}

	stderr := cmd.ErrOrStderr()
	store, sess, err := loadHookSession(id, name)
	if err != nil {
		piHookWarn(stderr, err)
	}
	if sess != nil {
		name = sess.Name // state files follow the record's current name
	}
	if name == "" {
		return nil // an id without a record: nothing to key a state file by
	}
	if isShutdown {
		// pi reports reason "quit" for every teardown that ends the process:
		// the user's /quit or ctrl+d as much as the SIGHUP that ks quit
		// sends, since interactive-mode.ts runs the same runtimeHost.dispose()
		// from its signal handler. No reason therefore tells an explicit exit
		// from the instance going away, and a record wrongly marked stopped
		// would not come back on the next attach. The record stays active,
		// as it does for a claude SessionEnd "other"; only the state file
		// goes. reload, new, resume and fork are followed by a session_start
		// that writes it again.
		state.Clean(name)
		return nil
	}
	if err := state.Write(name, s); err != nil {
		piHookWarn(stderr, err)
	}
	if sess != nil && payload.Event == "session_start" {
		recordPiSession(stderr, store, sess, payload)
	}
	return nil
}

// piStateForEvent maps an event the ks pi extension reports to the state ks
// records for it, or "" when ks ignores the event. The extension sends
// agent_settled only once pi is idle, and blocked/unblocked when its
// permission gate starts and stops waiting on the user.
func piStateForEvent(event string) string {
	switch event {
	case "agent_start", "tool_call", "unblocked":
		return "working"
	case "agent_settled":
		return "idle"
	case "blocked":
		return "input"
	case "session_start":
		return "waiting"
	}
	return ""
}

// recordPiSession stores pi's session id and session file on the ks session
// so a later reopen can pi --session it, and marks the session active again.
func recordPiSession(
	stderr io.Writer,
	store *session.Store,
	sess *session.Session,
	p piHookPayload,
) {
	if p.SessionID == "" {
		return
	}
	sess.PiSessionID = p.SessionID
	sess.PiSessionPath = p.SessionFile
	sess.Status = session.StatusActive
	if err := store.Save(sess); err != nil {
		piHookWarn(stderr, err)
	}
}

// piHookWarn reports a non-fatal _pi-hook problem on stderr.
func piHookWarn(stderr io.Writer, err error) {
	fmt.Fprintf(stderr, "ks _pi-hook: %v\n", err)
}
