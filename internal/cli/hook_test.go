package cli

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/mad01/kitty-session/internal/session"
	"github.com/mad01/kitty-session/internal/state"
)

// hookCase is one TestHook row. session is KS_SESSION_NAME; record, when set,
// is the name the seed is saved under (and checked against) when that differs
// from the env var, as after a rename.
type hookCase struct {
	name    string
	session string
	id      string // KS_SESSION_ID
	nested  bool   // the fake process tree says a nested claude fired the hook
	payload string
	// wantState is the state expected under the resolved record's name; ""
	// means no state file may be written.
	wantState string
	seedState string // state file written before the hook runs
	seed      *session.Session
	record    string
	wantStatus,
	wantClaudeID,
	wantTranscript string
}

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

	for _, tc := range hookCases() {
		t.Run(tc.name, func(t *testing.T) {
			runHookCase(t, store, tc)
		})
	}
}

func hookCases() []hookCase {
	return []hookCase{
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
			name:           "SessionStart records claude id and transcript and reactivates",
			session:        "ks-hook-test-start-record",
			payload:        `{"hook_event_name":"SessionStart","source":"resume","session_id":"abc-123","transcript_path":"/home/u/.claude/projects/-tmp/abc-123.jsonl","cwd":"/tmp"}`,
			wantState:      "waiting",
			seed:           &session.Session{Status: session.StatusStopped, ClaudeSessionID: "old"},
			wantStatus:     session.StatusActive,
			wantClaudeID:   "abc-123",
			wantTranscript: "/home/u/.claude/projects/-tmp/abc-123.jsonl",
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
			name:         "SessionStart from a subagent writes state but not the record",
			session:      "ks-hook-test-start-agent",
			payload:      `{"hook_event_name":"SessionStart","source":"startup","agent_id":"agent-7","session_id":"sub-1","transcript_path":"/t","cwd":"/tmp"}`,
			wantState:    "waiting",
			seed:         &session.Session{Status: session.StatusActive, ClaudeSessionID: "main-1"},
			wantStatus:   session.StatusActive,
			wantClaudeID: "main-1",
		},
		{
			name:           "KS_SESSION_ID finds a renamed record and keys state by its new name",
			session:        "ks-hook-test-old-name",
			id:             "id-renamed",
			payload:        `{"hook_event_name":"SessionStart","source":"startup","session_id":"new-1","transcript_path":"/t","cwd":"/tmp"}`,
			wantState:      "waiting",
			seed:           &session.Session{ID: "id-renamed", Status: session.StatusStopped},
			record:         "ks-hook-test-new-name",
			wantStatus:     session.StatusActive,
			wantClaudeID:   "new-1",
			wantTranscript: "/t",
		},
		{
			name:           "unknown KS_SESSION_ID falls back to the name",
			session:        "ks-hook-test-by-name",
			id:             "id-nobody-has",
			payload:        `{"hook_event_name":"SessionStart","source":"startup","session_id":"n-1","transcript_path":"/t","cwd":"/tmp"}`,
			wantState:      "waiting",
			seed:           &session.Session{Status: session.StatusStopped},
			wantStatus:     session.StatusActive,
			wantClaudeID:   "n-1",
			wantTranscript: "/t",
		},
		{
			name:         "SessionEnd prompt_input_exit marks stopped and drops the state file",
			session:      "ks-hook-test-end-exit",
			payload:      `{"hook_event_name":"SessionEnd","reason":"prompt_input_exit","session_id":"s1","transcript_path":"/t","cwd":"/tmp"}`,
			seedState:    "idle",
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
			name:       "SessionEnd from a subagent leaves the record alone",
			session:    "ks-hook-test-end-agent",
			payload:    `{"hook_event_name":"SessionEnd","reason":"prompt_input_exit","agent_id":"agent-7","session_id":"s1","transcript_path":"/t","cwd":"/tmp"}`,
			seed:       &session.Session{Status: session.StatusActive},
			wantStatus: session.StatusActive,
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
			name:       "nested claude writes nothing",
			session:    "ks-hook-test-nested",
			nested:     true,
			payload:    `{"hook_event_name":"SessionStart","source":"startup","session_id":"inner","transcript_path":"/t","cwd":"/tmp"}`,
			seed:       &session.Session{Status: session.StatusStopped},
			wantStatus: session.StatusStopped,
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
}

func runHookCase(t *testing.T, store *session.Store, tc hookCase) {
	t.Helper()
	record := tc.record
	if record == "" {
		record = tc.session
	}
	if tc.seed != nil {
		tc.seed.Name = record
		tc.seed.Dir = "/tmp"
		if err := store.Save(tc.seed); err != nil {
			t.Fatal(err)
		}
	}
	if tc.seedState != "" {
		if err := state.Write(record, tc.seedState); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("KS_SESSION_NAME", tc.session)
	t.Setenv("KS_SESSION_ID", tc.id)
	useFakeProcessTree(t, tc.nested)

	var stderr bytes.Buffer
	rootCmd.SetIn(strings.NewReader(tc.payload))
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"_hook"})
	t.Cleanup(func() {
		rootCmd.SetIn(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})

	before := stateFileCount(t)
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if tc.seedState != "" {
		if _, _, err := state.Read(record); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("state file for %q still present (err %v), want removed", record, err)
		}
		return
	}
	checkState(t, record, tc.wantState, before, stateFileCount(t))
	if tc.seed != nil {
		checkRecord(t, store, record, tc)
	}
	if tc.seed == nil && tc.session != "" && !tc.nested && tc.wantState != "" {
		// A missing record is reported, never swallowed.
		if !strings.Contains(stderr.String(), "ks _hook:") {
			t.Errorf("stderr = %q, want a ks _hook: line about the missing record", stderr.String())
		}
	}
}

// useFakeProcessTree points the hook at a fake tree where the hook's real
// parent (the test binary) is either the Claude ks launched or a nested one.
func useFakeProcessTree(t *testing.T, nested bool) {
	t.Helper()
	self := os.Getppid()
	procs := map[int]fakeProc{
		pidKitty:    {ppid: 1, comm: "kitty"},
		pidClaude:   {ppid: pidKitty, comm: "claude"},
		pidBashTool: {ppid: pidClaude, comm: "zsh"},
		self:        {ppid: pidKitty, comm: "claude"},
	}
	if nested {
		procs[self] = fakeProc{ppid: pidBashTool, comm: "claude"}
	}
	prev := hookProcess
	hookProcess = fakeTree(procs)
	t.Cleanup(func() { hookProcess = prev })
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

func checkRecord(t *testing.T, store *session.Store, name string, tc hookCase) {
	t.Helper()
	got, err := store.Load(name)
	if err != nil {
		t.Fatalf("Load(%q): %v", name, err)
	}
	if got.Status != tc.wantStatus {
		t.Errorf("Status = %q, want %q", got.Status, tc.wantStatus)
	}
	if got.ClaudeSessionID != tc.wantClaudeID {
		t.Errorf("ClaudeSessionID = %q, want %q", got.ClaudeSessionID, tc.wantClaudeID)
	}
	if got.ClaudeTranscriptPath != tc.wantTranscript {
		t.Errorf("ClaudeTranscriptPath = %q, want %q", got.ClaudeTranscriptPath, tc.wantTranscript)
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
