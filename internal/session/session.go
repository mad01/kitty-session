// Package session defines the Session record ks keeps for every kitty tab it
// manages and the file-backed Store that persists those records under
// ~/.config/ks/sessions/.
package session

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// Lifecycle values for Session.Status. An empty Status, as written by ks
// versions that predate the field, means active.
const (
	// StatusActive marks a session the user has not stopped. ks will
	// reopen it after kitty goes away.
	StatusActive = "active"
	// StatusStopped marks a session the user ended, via ks close --keep,
	// the TUI close action, or an explicit /exit inside claude.
	StatusStopped = "stopped"
)

// Agent kinds for Session.Agent: the program in the session's right-hand
// window. An empty Agent, as written by ks versions that predate the field,
// means claude; use Kind to read it.
const (
	AgentClaude = "claude"
	AgentPi     = "pi"
	AgentShell  = "shell"
)

// Kinds lists every agent kind, claude first as the default, in the order
// the CLI validates and the sidebar's chooser offers them.
var Kinds = []string{AgentClaude, AgentPi, AgentShell}

// idBytes is the length of a random session ID before hex encoding.
const idBytes = 16

// Session is one ks-managed kitty tab in the ks instance: a sidebar window
// beside a claude window. The kitty IDs are ephemeral and go stale when the
// instance restarts; ID, ClaudeSessionID and Status survive that and let ks
// find the record and resume the conversation.
type Session struct {
	// ID identifies the record across renames. The launcher exports it as
	// KS_SESSION_ID so the hook finds the record whatever its current name.
	// Records from before the field have none until the next reopen.
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
	Dir  string `json:"dir"`
	// CreatedAt is RFC 3339 UTC with nanoseconds, so records made in the same
	// second still order; records from older versions have whole seconds.
	CreatedAt            string `json:"created_at"`
	KittyTabID           int    `json:"kitty_tab_id"`
	KittyWindowID        int    `json:"kitty_window_id,omitempty"`
	KittySidebarWindowID int    `json:"kitty_sidebar_window_id,omitempty"`
	// KittyShellWindowID and KittySummaryWindowID were written by the
	// pre-instance topology. New records never set them.
	KittyShellWindowID   int `json:"kitty_shell_window_id,omitempty"`
	KittySummaryWindowID int `json:"kitty_summary_window_id,omitempty"`
	// Status is StatusActive or StatusStopped; see IsActive for the empty case.
	Status string `json:"status,omitempty"`
	// ClaudeSessionID is the session_id Claude Code reported on its last
	// SessionStart hook, used for claude --resume on reopen.
	ClaudeSessionID string `json:"claude_session_id,omitempty"`
	// ClaudeTranscriptPath is the transcript_path from that same hook. The
	// launcher only resumes while this file still exists.
	ClaudeTranscriptPath string `json:"claude_transcript_path,omitempty"`
	// Agent is the program in the session's right-hand window: AgentClaude,
	// AgentPi or AgentShell. Empty means claude; read it through Kind.
	Agent string `json:"agent,omitempty"`
	// PiSessionID and PiSessionPath are pi's session id and session file
	// from the last session_start the ks pi extension reported, used for
	// pi --session on reopen while the file still exists. Only AgentPi
	// records carry them.
	PiSessionID   string `json:"pi_session_id,omitempty"`
	PiSessionPath string `json:"pi_session_path,omitempty"`
	// FocusedAt is when the launcher last created or focused the session.
	// Attach brings the most recently focused session to the front.
	FocusedAt time.Time `json:"focused_at,omitzero"`
	// ViewedAt is when the session's own sidebar last saw its tab as the
	// active one. A finished turn (state file idle) newer than this shows as
	// done in the sidebar until the user looks at the tab.
	ViewedAt time.Time `json:"viewed_at,omitzero"`
	// Position is the session's rank in the sidebar order, 1-based and dense
	// over the sessions with a tab. ks move writes it, and once any session
	// carries one every tab launch ranks the open sessions again, so a
	// reopened or new session takes the bottom rank. Zero means no ks move
	// has run yet; attach puts ranked sessions first, by rank, and the rest
	// after them in creation order.
	Position int `json:"position,omitempty"`
}

// New returns an active session record for name rooted at dir, with a fresh ID.
func New(name, dir string, tabID, windowID int) *Session {
	return &Session{
		ID:            NewID(),
		Name:          name,
		Dir:           dir,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339Nano),
		KittyTabID:    tabID,
		KittyWindowID: windowID,
		Status:        StatusActive,
	}
}

// NewID returns a random session ID: idBytes bytes, hex encoded.
func NewID() string {
	b := make([]byte, idBytes)
	if _, err := rand.Read(b); err != nil {
		panic("session: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// Kind returns the session's agent kind, AgentClaude for records written
// before the field existed.
func (s *Session) Kind() string {
	if s.Agent == "" {
		return AgentClaude
	}
	return s.Agent
}

// IsActive reports whether the user has not stopped the session. Records
// written before Status existed have an empty Status and count as active.
func (s *Session) IsActive() bool {
	return s.Status == "" || s.Status == StatusActive
}
