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

// idBytes is the length of a random session ID before hex encoding.
const idBytes = 16

// Session is one ks-managed kitty tab pairing claude with a shell. The kitty
// IDs are ephemeral and go stale when kitty restarts; ID, ClaudeSessionID and
// Status survive that and let ks find the record and resume the conversation.
type Session struct {
	// ID identifies the record across renames. The launcher exports it as
	// KS_SESSION_ID so the hook finds the record whatever its current name.
	// Records from before the field have none until the next reopen.
	ID                   string `json:"id,omitempty"`
	Name                 string `json:"name"`
	Dir                  string `json:"dir"`
	CreatedAt            string `json:"created_at"`
	KittyTabID           int    `json:"kitty_tab_id"`
	KittyWindowID        int    `json:"kitty_window_id,omitempty"`
	KittyShellWindowID   int    `json:"kitty_shell_window_id,omitempty"`
	KittySummaryWindowID int    `json:"kitty_summary_window_id,omitempty"`
	// Status is StatusActive or StatusStopped; see IsActive for the empty case.
	Status string `json:"status,omitempty"`
	// ClaudeSessionID is the session_id Claude Code reported on its last
	// SessionStart hook, used for claude --resume on reopen.
	ClaudeSessionID string `json:"claude_session_id,omitempty"`
	// ClaudeTranscriptPath is the transcript_path from that same hook. The
	// launcher only resumes while this file still exists.
	ClaudeTranscriptPath string `json:"claude_transcript_path,omitempty"`
}

// New returns an active session record for name rooted at dir, with a fresh ID.
func New(name, dir string, tabID, windowID int) *Session {
	return &Session{
		ID:            NewID(),
		Name:          name,
		Dir:           dir,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
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

// IsActive reports whether the user has not stopped the session. Records
// written before Status existed have an empty Status and count as active.
func (s *Session) IsActive() bool {
	return s.Status == "" || s.Status == StatusActive
}
