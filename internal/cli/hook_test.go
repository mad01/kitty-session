package cli

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/mad01/kitty-session/internal/session"
	"github.com/mad01/kitty-session/internal/state"
)

// All hook cases share one fake HOME: state.Dir resolves HOME once per process,
// so a second test function with its own HOME would write into a removed dir.
func TestHook(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Fail loudly if another test already pinned the state dir elsewhere, so
	// we never write into the real state dir.
	if dir := state.Dir(); !strings.HasPrefix(dir, home) {
		t.Fatalf("state.Dir() = %s, want a path under %s", dir, home)
	}
	store, err := session.NewStore()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		session   string
		payload   string
		wantState string // "" means no state file may be written
		// seed, when set, is saved as the session record before the hook
		// runs; wantStatus and wantClaudeID are then checked against the
		// reloaded record.
		seed         *session.Session
		wantStatus   string
		wantClaudeID string
	}{
		{
			name:      "PreToolUse writes working",
			session:   "ks-hook-test-pretool",
			payload:   `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"ls"},"session_id":"s1","transcript_path":"/t","cwd":"/tmp","permission_mode":"default"}`,
			wantState: "working",
		},
		{
			name:      "Stop writes idle",
			session:   "ks-hook-test-stop",
			payload:   `{"hook_event_name":"Stop","last_assistant_message":"done","session_id":"s1","transcript_path":"/t","cwd":"/tmp"}`,
			wantState: "idle",
		},
		{
			name:      "Notification permission_prompt writes input",
			session:   "ks-hook-test-perm",
			payload:   `{"hook_event_name":"Notification","notification_type":"permission_prompt","session_id":"s1","transcript_path":"/t","cwd":"/tmp"}`,
			wantState: "input",
		},
		{
			name:      "Notification elicitation_dialog writes input",
			session:   "ks-hook-test-elicit",
			payload:   `{"hook_event_name":"Notification","notification_type":"elicitation_dialog","session_id":"s1","transcript_path":"/t","cwd":"/tmp"}`,
			wantState: "input",
		},
		{
			name:      "SessionStart without a session record still writes waiting",
			session:   "ks-hook-test-start",
			payload:   `{"hook_event_name":"SessionStart","source":"startup","session_id":"s1","transcript_path":"/t","cwd":"/tmp"}`,
			wantState: "waiting",
		},
		{
			name:         "SessionStart records the claude session id and reactivates",
			session:      "ks-hook-test-start-record",
			payload:      `{"hook_event_name":"SessionStart","source":"resume","session_id":"abc-123","transcript_path":"/t","cwd":"/tmp"}`,
			wantState:    "waiting",
			seed:         &session.Session{Status: session.StatusStopped, ClaudeSessionID: "old"},
			wantStatus:   session.StatusActive,
			wantClaudeID: "abc-123",
		},
		{
			name:         "SessionStart with empty session id leaves the record alone",
			session:      "ks-hook-test-start-empty",
			payload:      `{"hook_event_name":"SessionStart","source":"startup","session_id":"","transcript_path":"/t","cwd":"/tmp"}`,
			wantState:    "waiting",
			seed:         &session.Session{Status: session.StatusStopped, ClaudeSessionID: "keep"},
			wantStatus:   session.StatusStopped,
			wantClaudeID: "keep",
		},
		{
			name:         "SessionEnd prompt_input_exit marks stopped",
			session:      "ks-hook-test-end-exit",
			payload:      `{"hook_event_name":"SessionEnd","reason":"prompt_input_exit","session_id":"s1","transcript_path":"/t","cwd":"/tmp"}`,
			seed:         &session.Session{Status: session.StatusActive, ClaudeSessionID: "id-1"},
			wantStatus:   session.StatusStopped,
			wantClaudeID: "id-1",
		},
		{
			name:       "SessionEnd logout marks stopped",
			session:    "ks-hook-test-end-logout",
			payload:    `{"hook_event_name":"SessionEnd","reason":"logout","session_id":"s1","transcript_path":"/t","cwd":"/tmp"}`,
			seed:       &session.Session{Status: session.StatusActive},
			wantStatus: session.StatusStopped,
		},
		{
			name:       "SessionEnd other (window closed) keeps active",
			session:    "ks-hook-test-end-other",
			payload:    `{"hook_event_name":"SessionEnd","reason":"other","session_id":"s1","transcript_path":"/t","cwd":"/tmp"}`,
			seed:       &session.Session{Status: session.StatusActive},
			wantStatus: session.StatusActive,
		},
		{
			name:       "SessionEnd clear keeps active",
			session:    "ks-hook-test-end-clear",
			payload:    `{"hook_event_name":"SessionEnd","reason":"clear","session_id":"s1","transcript_path":"/t","cwd":"/tmp"}`,
			seed:       &session.Session{Status: session.StatusActive},
			wantStatus: session.StatusActive,
		},
		{
			name:    "SessionEnd without a session record is silent",
			session: "ks-hook-test-end-missing",
			payload: `{"hook_event_name":"SessionEnd","reason":"prompt_input_exit","session_id":"s1","transcript_path":"/t","cwd":"/tmp"}`,
		},
		{
			name:    "Notification idle_prompt writes nothing",
			session: "ks-hook-test-idle-prompt",
			payload: `{"hook_event_name":"Notification","notification_type":"idle_prompt","session_id":"s1","transcript_path":"/t","cwd":"/tmp"}`,
		},
		{
			name:    "legacy event shape writes nothing",
			session: "ks-hook-test-legacy",
			payload: `{"event":"Stop","tool":{"name":"Bash"},"notification":{"type":"permission_prompt"}}`,
		},
		{
			name:    "empty KS_SESSION_NAME writes nothing",
			session: "",
			payload: `{"hook_event_name":"Stop","session_id":"s1","transcript_path":"/t","cwd":"/tmp"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.seed != nil {
				tc.seed.Name = tc.session
				tc.seed.Dir = "/tmp"
				if err := store.Save(tc.seed); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("KS_SESSION_NAME", tc.session)
			rootCmd.SetIn(strings.NewReader(tc.payload))
			rootCmd.SetArgs([]string{"_hook"})
			t.Cleanup(func() {
				rootCmd.SetIn(nil)
				rootCmd.SetArgs(nil)
			})

			before := stateFileCount(t)
			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("execute error: %v", err)
			}
			after := stateFileCount(t)

			checkState(t, tc.session, tc.wantState, before, after)
			if tc.seed != nil {
				checkRecord(t, store, tc.session, tc.wantStatus, tc.wantClaudeID)
			}
		})
	}
}

// checkState asserts the state file for name holds want, or that no state
// file was written when want is "".
func checkState(t *testing.T, name, want string, before, after int) {
	t.Helper()
	if want == "" {
		if after != before {
			t.Fatalf("state dir grew from %d to %d files, want unchanged", before, after)
		}
		if _, _, err := state.Read(name); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("state.Read(%q) err = %v, want not-exist", name, err)
		}
		return
	}
	got, updatedAt, err := state.Read(name)
	if err != nil {
		t.Fatalf("state.Read(%q): %v", name, err)
	}
	if got != want {
		t.Errorf("state = %q, want %q", got, want)
	}
	if !state.IsFresh(updatedAt) {
		t.Errorf("updated_at %v is not fresh", updatedAt)
	}
}

func checkRecord(t *testing.T, store *session.Store, name, wantStatus, wantClaudeID string) {
	t.Helper()
	got, err := store.Load(name)
	if err != nil {
		t.Fatalf("Load(%q): %v", name, err)
	}
	if got.Status != wantStatus {
		t.Errorf("Status = %q, want %q", got.Status, wantStatus)
	}
	if got.ClaudeSessionID != wantClaudeID {
		t.Errorf("ClaudeSessionID = %q, want %q", got.ClaudeSessionID, wantClaudeID)
	}
}

func stateFileCount(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir(state.Dir())
	if err != nil {
		t.Fatalf("read state dir: %v", err)
	}
	return len(entries)
}
