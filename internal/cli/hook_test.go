package cli

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/mad01/kitty-session/internal/state"
)

func TestHookWritesState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// state.Dir resolves HOME once per process. Fail loudly if another test
	// already pinned it elsewhere, so we never write into the real state dir.
	if dir := state.Dir(); !strings.HasPrefix(dir, home) {
		t.Fatalf("state.Dir() = %s, want a path under %s", dir, home)
	}

	tests := []struct {
		name      string
		session   string
		payload   string
		wantState string // "" means no state file may be written
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
			name:      "SessionStart writes waiting",
			session:   "ks-hook-test-start",
			payload:   `{"hook_event_name":"SessionStart","source":"startup","session_id":"s1","transcript_path":"/t","cwd":"/tmp"}`,
			wantState: "waiting",
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

			if tc.wantState == "" {
				if after != before {
					t.Fatalf("state dir grew from %d to %d files, want unchanged", before, after)
				}
				if _, _, err := state.Read(tc.session); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("state.Read(%q) err = %v, want not-exist", tc.session, err)
				}
				return
			}

			got, updatedAt, err := state.Read(tc.session)
			if err != nil {
				t.Fatalf("state.Read(%q): %v", tc.session, err)
			}
			if got != tc.wantState {
				t.Errorf("state = %q, want %q", got, tc.wantState)
			}
			if !state.IsFresh(updatedAt) {
				t.Errorf("updated_at %v is not fresh", updatedAt)
			}
		})
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
