package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/mad01/kitty-session/internal/hooks"
	"github.com/mad01/kitty-session/internal/procinfo"
	"github.com/mad01/kitty-session/internal/session"
	"github.com/mad01/kitty-session/internal/state"
	"github.com/spf13/cobra"
)

// hookPayload is the subset of the JSON object Claude Code pipes to hook
// commands on stdin. Every event carries hook_event_name, session_id and
// transcript_path. tool_name is set for PreToolUse, PostToolUse,
// PostToolUseFailure and PermissionRequest, notification_type for
// Notification and reason for SessionEnd; other events leave them empty.
// Elicitation and ElicitationResult carry mcp_server_name instead of a tool
// name; toolOf turns it into the prefix every tool of that server shares.
type hookPayload struct {
	HookEventName    string `json:"hook_event_name"`
	SessionID        string `json:"session_id"`
	TranscriptPath   string `json:"transcript_path"`
	ToolName         string `json:"tool_name"`
	NotificationType string `json:"notification_type"`
	Reason           string `json:"reason"`
	MCPServerName    string `json:"mcp_server_name"`
	// AgentID is set when an in-process subagent fired the event. What a
	// subagent may write is allowWrite's call, and it never touches the
	// record.
	AgentID string `json:"agent_id"`
}

// inputTools are the tools that wait on the user without a permission
// dialog, so under bypass-permissions mode neither PermissionRequest nor
// Notification fires for them; PreToolUse is the one event that sees them.
var inputTools = []string{"AskUserQuestion", "ExitPlanMode"}

// hookCmd prints nothing on stdout: on PermissionRequest Claude reads a
// decision there, and none means the usual dialog.
var hookCmd = &cobra.Command{
	Use:    "_hook",
	Short:  "Handle Claude Code hook events",
	Hidden: true,
	RunE:   runHook,
}

func init() {
	rootCmd.AddCommand(hookCmd)
}

// runHook handles one Claude Code hook event. It exits 0 whatever happens to
// the session record: the state file is the hook's job, and a record problem
// must not fail Claude's hook run. Such problems go to stderr instead.
func runHook(cmd *cobra.Command, args []string) error {
	name := os.Getenv("KS_SESSION_NAME")
	if name == "" {
		return nil // not inside a ks session, nothing to do
	}
	stderr := cmd.ErrOrStderr()
	top, err := firedByTopLevelClaude(hookProcess, os.Getppid())
	if err != nil && !errors.Is(err, procinfo.ErrUnsupported) {
		hookWarn(stderr, err) // undecidable: a stray write beats a dead badge
	}
	if err == nil && !top {
		return nil // a claude nested inside the session, not the one ks launched
	}

	payload, err := readHookPayload[hookPayload](cmd.InOrStdin())
	if err != nil {
		return err
	}
	isEnd := payload.HookEventName == "SessionEnd"
	s := stateForEvent(payload)
	if !isEnd && s == "" {
		return nil
	}

	store, sess, err := loadHookSession(os.Getenv("KS_SESSION_ID"), name)
	if err != nil {
		hookWarn(stderr, err)
	}
	if sess != nil {
		name = sess.Name // state files follow the record's current name
	}
	if isEnd {
		if sess != nil && payload.AgentID == "" && userExit(payload.Reason) {
			markSessionStopped(stderr, store, sess)
			if err := hookPoke(sess); err != nil {
				hookWarn(stderr, err)
			}
		}
		return nil
	}
	writeState(stderr, name, payload, s)
	if sess != nil && payload.HookEventName == "SessionStart" && payload.AgentID == "" {
		recordClaudeSession(stderr, store, sess, payload)
	}
	return nil
}

// writeState replaces the session's state file with the event's state when
// allowWrite lets it. The current entry is read first; one that cannot be
// read counts as none and is reported.
func writeState(stderr io.Writer, name string, p hookPayload, s string) {
	cur, err := state.ReadEntry(name)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		hookWarn(stderr, err)
	}
	if !allowWrite(cur, p, s) {
		return
	}
	if err := state.WriteEntry(name, entryFor(p, s)); err != nil {
		hookWarn(stderr, err)
	}
}

// allowWrite decides whether the event may replace the current state file
// entry cur with state s. An input entry is sticky and remembers who asked
// (entryFor); only the asker's own completion clears it, so a tool finishing
// elsewhere never takes a dialog off the badge while it is still on screen.
//
// A parent event (no agent_id) always writes, except a completion (a tool
// finishing in PostToolUse or PostToolUseFailure, an MCP dialog answered in
// ElicitationResult), which leaves an input alone unless the parent asked
// it, for that tool or for no tool in particular: in a parallel batch Read
// finishing must not clear the dialog Bash waits on, and a subagent's
// prompt is not the parent's to clear.
//
// A subagent event writes its own input always, stamped with its agent_id
// and tool; its completion clears only an input it asked itself, for that
// tool or for none; anything else from a subagent is ignored, since its
// activity says nothing about the parent's turn, whose own Stop ends it.
func allowWrite(cur state.Entry, p hookPayload, s string) bool {
	done := isCompletion(p.HookEventName)
	tool := toolOf(p)
	if p.AgentID == "" {
		return !done || cur.State != "input" || ownsInput(cur, "", tool)
	}
	if s == "input" {
		return true
	}
	return done && cur.State == "input" && ownsInput(cur, p.AgentID, tool)
}

// isCompletion reports whether the event ends something an input entry may
// be waiting on: a tool call, or an MCP server's dialog.
func isCompletion(event string) bool {
	switch event {
	case "PostToolUse", "PostToolUseFailure", "ElicitationResult":
		return true
	}
	return false
}

// toolOf is the tool the event concerns: tool_name, or for the MCP dialog
// events the mcp__<server>__ prefix every tool of that server carries, so
// the dialog's input is owned by whichever tool call of that server opened
// it and by the dialog's own answer.
func toolOf(p hookPayload) string {
	switch p.HookEventName {
	case "Elicitation", "ElicitationResult":
		return "mcp__" + p.MCPServerName + "__"
	}
	return p.ToolName
}

// ownsInput reports whether the input entry cur was asked by agent ("" for
// the parent) for tool: the same tool, no tool (a Notification, which counts
// for every tool), or a server prefix (mcp__<server>__, from an Elicitation)
// that tool carries.
func ownsInput(cur state.Entry, agent, tool string) bool {
	if cur.AgentID != agent {
		return false
	}
	if cur.Tool == "" || cur.Tool == tool {
		return true
	}
	return strings.HasSuffix(cur.Tool, "__") && strings.HasPrefix(tool, cur.Tool)
}

// entryFor is the state file content for the event: an input entry names
// its asker and the tool the prompt is for (none for a Notification, the
// server prefix for an Elicitation), so a later completion can tell whether
// it is theirs; every other state is bare.
func entryFor(p hookPayload, s string) state.Entry {
	if s != "input" {
		return state.Entry{State: s}
	}
	return state.Entry{State: s, AgentID: p.AgentID, Tool: toolOf(p)}
}

// readHookPayload decodes the JSON object a hook handler is given on stdin:
// hookPayload for _hook, piHookPayload for _pi-hook.
func readHookPayload[T any](r io.Reader) (T, error) {
	var payload T
	data, err := io.ReadAll(r)
	if err != nil {
		return payload, fmt.Errorf("cannot read stdin: %w", err)
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return payload, fmt.Errorf("cannot parse hook payload: %w", err)
	}
	return payload, nil
}

// stateForEvent maps a hook event to the state ks records for it, or "" when
// ks ignores the event:
//
//	UserPromptSubmit                                → working
//	PreToolUse                                      → input for a tool in inputTools, else working
//	PostToolUse, PostToolUseFailure                 → working: the turn goes on after the tool
//	PermissionRequest                               → input: Claude is about to show the dialog
//	Elicitation                                     → input: an MCP server's dialog is about to show
//	ElicitationResult                               → working: the dialog was answered, the turn goes on (a completion: clears only the dialog's own input)
//	Stop                                            → idle
//	StopFailure                                     → error: the turn died on an API error
//	Notification permission_prompt or elicitation_dialog → input
//	SessionStart                                    → waiting
//
// Whether the state reaches the file is allowWrite's call, which is where a
// subagent's events are weighed. SessionEnd writes no state; runHook
// handles it.
func stateForEvent(p hookPayload) string {
	switch p.HookEventName {
	case "UserPromptSubmit", "PostToolUse", "PostToolUseFailure", "ElicitationResult":
		return "working"
	case "PreToolUse":
		if slices.Contains(inputTools, p.ToolName) {
			return "input"
		}
		return "working"
	case "PermissionRequest", "Elicitation":
		return "input"
	case "Stop":
		return "idle"
	case "StopFailure":
		return "error"
	case "Notification":
		switch p.NotificationType {
		case "permission_prompt", "elicitation_dialog":
			return "input"
		}
		return ""
	case "SessionStart":
		return "waiting"
	}
	return ""
}

// loadHookSession returns the record the event belongs to, or a nil session
// with the reason as the error; the store still comes back when only the
// lookup failed. Lookup is by KS_SESSION_ID first, so a renamed session still
// finds its record, then by KS_SESSION_NAME for records that predate the id
// (findSession). Both hook handlers share it.
func loadHookSession(id, name string) (*session.Store, *session.Session, error) {
	store, err := session.NewStore()
	if err != nil {
		return nil, nil, err
	}
	sess, err := findSession(store, id, name)
	if err != nil {
		return store, nil, err
	}
	return store, sess, nil
}

// recordClaudeSession stores Claude's session_id and transcript path on the
// ks session so a later reopen can claude --resume it, and marks the session
// active again.
func recordClaudeSession(
	stderr io.Writer,
	store *session.Store,
	sess *session.Session,
	p hookPayload,
) {
	if p.SessionID == "" {
		return
	}
	sess.ClaudeSessionID = p.SessionID
	sess.ClaudeTranscriptPath = p.TranscriptPath
	sess.Status = session.StatusActive
	if err := store.Save(sess); err != nil {
		hookWarn(stderr, err)
	}
}

// userExit reports whether a SessionEnd reason is the user ending the agent
// at its prompt: /exit, ctrl+c twice or ctrl+d (prompt_input_exit) and
// logout. A window closed by kitty (other) or a /clear or /resume restart
// are not.
func userExit(reason string) bool {
	return reason == hooks.ReasonPromptInputExit || reason == hooks.ReasonLogout
}

// markSessionStopped records that the user ended the agent and drops the
// session's state file, so the next attach leaves the session alone.
func markSessionStopped(stderr io.Writer, store *session.Store, sess *session.Session) {
	sess.Status = session.StatusStopped
	if err := store.Save(sess); err != nil {
		hookWarn(stderr, err)
	}
	state.Clean(sess.Name)
}

// hookWarn reports a non-fatal hook problem on stderr.
func hookWarn(stderr io.Writer, err error) {
	fmt.Fprintf(stderr, "ks _hook: %v\n", err)
}
