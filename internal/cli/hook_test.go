package cli

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

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
	// wantState, wantAgent and wantTool are the entry expected under the
	// resolved record's name; a "" wantState means no state file may exist
	// afterwards: none written, or, with a seedFile, the seeded one removed.
	wantState, wantAgent, wantTool string
	// seedFile is the state file written before the hook runs; zero for none.
	seedFile state.Entry
	// quiet means the event must leave everything alone: the state file
	// keeps its seeded content, the seeded record is untouched and nothing
	// goes to stderr.
	quiet  bool
	seed   *session.Session
	record string
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
	// The pi handler's cases run here for the same reason (pihook_test.go).
	for _, tc := range piHookCases() {
		t.Run(tc.name, func(t *testing.T) {
			runPiHookCase(t, store, tc)
		})
	}
}

func hookCases() []hookCase {
	return []hookCase{
		{
			name:      "UserPromptSubmit writes working",
			session:   "ks-hook-test-prompt",
			payload:   `{"hook_event_name":"UserPromptSubmit","prompt":"go on","session_id":"s1","transcript_path":"/t","cwd":"/tmp"}`,
			wantState: "working",
		},
		{
			name:      "PreToolUse Bash writes working",
			session:   "ks-hook-test-pretool",
			payload:   `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"ls"},"session_id":"s1","transcript_path":"/t","cwd":"/tmp","permission_mode":"default"}`,
			wantState: "working",
		},
		{
			name:      "PreToolUse AskUserQuestion writes input",
			session:   "ks-hook-test-ask",
			payload:   `{"hook_event_name":"PreToolUse","tool_name":"AskUserQuestion","tool_input":{"questions":[]},"session_id":"s1","transcript_path":"/t","cwd":"/tmp","permission_mode":"bypassPermissions"}`,
			wantState: "input",
			wantTool:  "AskUserQuestion",
		},
		{
			name:      "PreToolUse ExitPlanMode writes input",
			session:   "ks-hook-test-plan",
			payload:   `{"hook_event_name":"PreToolUse","tool_name":"ExitPlanMode","tool_input":{},"session_id":"s1","transcript_path":"/t","cwd":"/tmp","permission_mode":"plan"}`,
			wantState: "input",
			wantTool:  "ExitPlanMode",
		},
		{
			name:      "PostToolUse writes working",
			session:   "ks-hook-test-posttool",
			payload:   `{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"ls"},"tool_response":{"stdout":"a"},"session_id":"s1","transcript_path":"/t","cwd":"/tmp"}`,
			wantState: "working",
		},
		{
			name:      "PostToolUseFailure writes working",
			session:   "ks-hook-test-posttool-fail",
			payload:   `{"hook_event_name":"PostToolUseFailure","tool_name":"Bash","tool_input":{"command":"false"},"error":"exit status 1","session_id":"s1","transcript_path":"/t","cwd":"/tmp"}`,
			wantState: "working",
		},
		{
			name:      "PermissionRequest writes input for its tool",
			session:   "ks-hook-test-permreq",
			payload:   `{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"rm -r build"},"session_id":"s1","transcript_path":"/t","cwd":"/tmp","permission_mode":"default"}`,
			wantState: "input",
			wantTool:  "Bash",
		},
		{
			name:      "Elicitation writes input for the server's tools",
			session:   "ks-hook-test-elicitation",
			payload:   `{"hook_event_name":"Elicitation","mcp_server_name":"linear","message":"Pick a team","mode":"form","session_id":"s1","transcript_path":"/t","cwd":"/tmp"}`,
			wantState: "input",
			wantTool:  "mcp__linear__",
		},
		{
			name:      "ElicitationResult clears the dialog's input",
			session:   "ks-hook-test-elicitation-result",
			payload:   `{"hook_event_name":"ElicitationResult","mcp_server_name":"linear","action":"accept","content":{"team":"core"},"mode":"form","session_id":"s1","transcript_path":"/t","cwd":"/tmp"}`,
			seedFile:  state.Entry{State: "input", Tool: "mcp__linear__"},
			wantState: "working",
		},
		{
			name:       "ElicitationResult leaves a Bash input alone",
			session:    "ks-hook-test-elicitation-other",
			payload:    `{"hook_event_name":"ElicitationResult","mcp_server_name":"linear","action":"accept","session_id":"s1","transcript_path":"/t","cwd":"/tmp"}`,
			seedFile:   state.Entry{State: "input", Tool: "Bash"},
			quiet:      true,
			seed:       &session.Session{Status: session.StatusActive},
			wantStatus: session.StatusActive,
		},
		{
			name:      "parent PostToolUse of the server's tool clears the dialog's input",
			session:   "ks-hook-test-elicitation-tool",
			payload:   `{"hook_event_name":"PostToolUse","tool_name":"mcp__linear__create_issue","tool_input":{},"tool_response":{},"session_id":"s1","transcript_path":"/t","cwd":"/tmp"}`,
			seedFile:  state.Entry{State: "input", Tool: "mcp__linear__"},
			wantState: "working",
		},
		{
			name:       "parent PostToolUse Bash leaves the dialog's input alone",
			session:    "ks-hook-test-elicitation-batch",
			payload:    `{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"ls"},"tool_response":{"stdout":"a"},"session_id":"s1","transcript_path":"/t","cwd":"/tmp"}`,
			seedFile:   state.Entry{State: "input", Tool: "mcp__linear__"},
			quiet:      true,
			seed:       &session.Session{Status: session.StatusActive},
			wantStatus: session.StatusActive,
		},
		{
			name:      "subagent Elicitation writes input with its agent and server",
			session:   "ks-hook-test-sub-elicitation",
			payload:   `{"hook_event_name":"Elicitation","mcp_server_name":"linear","message":"Pick a team","agent_id":"agent-7","session_id":"sub-1","transcript_path":"/t","cwd":"/tmp"}`,
			wantState: "input",
			wantAgent: "agent-7",
			wantTool:  "mcp__linear__",
		},
		{
			name:      "subagent ElicitationResult clears its own dialog's input",
			session:   "ks-hook-test-sub-elicitation-result",
			payload:   `{"hook_event_name":"ElicitationResult","mcp_server_name":"linear","action":"accept","agent_id":"agent-7","session_id":"sub-1","transcript_path":"/t","cwd":"/tmp"}`,
			seedFile:  state.Entry{State: "input", AgentID: "agent-7", Tool: "mcp__linear__"},
			wantState: "working",
		},
		{
			name:       "subagent ElicitationResult leaves the parent's dialog input",
			session:    "ks-hook-test-sub-elicitation-parent",
			payload:    `{"hook_event_name":"ElicitationResult","mcp_server_name":"linear","action":"accept","agent_id":"agent-7","session_id":"sub-1","transcript_path":"/t","cwd":"/tmp"}`,
			seedFile:   state.Entry{State: "input", Tool: "mcp__linear__"},
			quiet:      true,
			seed:       &session.Session{Status: session.StatusActive},
			wantStatus: session.StatusActive,
		},
		{
			name:      "subagent PermissionRequest writes input with its agent and tool",
			session:   "ks-hook-test-sub-permreq",
			payload:   `{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"make"},"agent_id":"agent-7","session_id":"sub-1","transcript_path":"/t","cwd":"/tmp"}`,
			wantState: "input",
			wantAgent: "agent-7",
			wantTool:  "Bash",
		},
		{
			name:      "subagent PostToolUse for its own tool clears its input",
			session:   "ks-hook-test-sub-clear",
			payload:   `{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"make"},"tool_response":{"stdout":"ok"},"agent_id":"agent-7","session_id":"sub-1","transcript_path":"/t","cwd":"/tmp"}`,
			seedFile:  state.Entry{State: "input", AgentID: "agent-7", Tool: "Bash"},
			wantState: "working",
		},
		{
			name:       "subagent PostToolUse for another tool leaves its input",
			session:    "ks-hook-test-sub-other",
			payload:    `{"hook_event_name":"PostToolUse","tool_name":"Read","tool_input":{"file_path":"/f"},"tool_response":{},"agent_id":"agent-7","session_id":"sub-1","transcript_path":"/t","cwd":"/tmp"}`,
			seedFile:   state.Entry{State: "input", AgentID: "agent-7", Tool: "Bash"},
			quiet:      true,
			seed:       &session.Session{Status: session.StatusActive},
			wantStatus: session.StatusActive,
		},
		{
			name:       "subagent PreToolUse Bash leaves a parent input alone",
			session:    "ks-hook-test-sub-pretool",
			payload:    `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"ls"},"agent_id":"agent-7","session_id":"sub-1","transcript_path":"/t","cwd":"/tmp"}`,
			seedFile:   state.Entry{State: "input", Tool: "Bash"},
			quiet:      true,
			seed:       &session.Session{Status: session.StatusActive},
			wantStatus: session.StatusActive,
		},
		{
			name:       "subagent Stop writes nothing",
			session:    "ks-hook-test-sub-stop",
			payload:    `{"hook_event_name":"Stop","agent_id":"agent-7","last_assistant_message":"done","session_id":"sub-1","transcript_path":"/t","cwd":"/tmp"}`,
			seedFile:   state.Entry{State: "working"},
			quiet:      true,
			seed:       &session.Session{Status: session.StatusActive},
			wantStatus: session.StatusActive,
		},
		{
			name:       "subagent StopFailure writes nothing",
			session:    "ks-hook-test-sub-stopfail",
			payload:    `{"hook_event_name":"StopFailure","agent_id":"agent-7","error":"API Error: 529 overloaded","session_id":"sub-1","transcript_path":"/t","cwd":"/tmp"}`,
			seedFile:   state.Entry{State: "working"},
			quiet:      true,
			seed:       &session.Session{Status: session.StatusActive},
			wantStatus: session.StatusActive,
		},
		{
			name:       "parent PostToolUse Read leaves a Bash input alone",
			session:    "ks-hook-test-batch",
			payload:    `{"hook_event_name":"PostToolUse","tool_name":"Read","tool_input":{"file_path":"/f"},"tool_response":{},"session_id":"s1","transcript_path":"/t","cwd":"/tmp"}`,
			seedFile:   state.Entry{State: "input", Tool: "Bash"},
			quiet:      true,
			seed:       &session.Session{Status: session.StatusActive},
			wantStatus: session.StatusActive,
		},
		{
			name:      "parent PostToolUse Bash clears its Bash input",
			session:   "ks-hook-test-batch-clear",
			payload:   `{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"make"},"tool_response":{"stdout":"ok"},"session_id":"s1","transcript_path":"/t","cwd":"/tmp"}`,
			seedFile:  state.Entry{State: "input", Tool: "Bash"},
			wantState: "working",
		},
		{
			name:      "parent PostToolUse clears a tool-less input",
			session:   "ks-hook-test-notify-clear",
			payload:   `{"hook_event_name":"PostToolUse","tool_name":"Read","tool_input":{"file_path":"/f"},"tool_response":{},"session_id":"s1","transcript_path":"/t","cwd":"/tmp"}`,
			seedFile:  state.Entry{State: "input"},
			wantState: "working",
		},
		{
			name:      "parent PreToolUse Bash overwrites a subagent input",
			session:   "ks-hook-test-parent-over",
			payload:   `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"ls"},"session_id":"s1","transcript_path":"/t","cwd":"/tmp"}`,
			seedFile:  state.Entry{State: "input", AgentID: "agent-7", Tool: "Bash"},
			wantState: "working",
		},
		{
			name:      "Stop writes idle",
			session:   "ks-hook-test-stop",
			payload:   `{"hook_event_name":"Stop","last_assistant_message":"done","session_id":"s1","transcript_path":"/t","cwd":"/tmp"}`,
			wantState: "idle",
		},
		{
			name:      "parent Stop writes idle over anything",
			session:   "ks-hook-test-stop-over",
			payload:   `{"hook_event_name":"Stop","last_assistant_message":"done","session_id":"s1","transcript_path":"/t","cwd":"/tmp"}`,
			seedFile:  state.Entry{State: "input", AgentID: "agent-7", Tool: "Bash"},
			wantState: "idle",
		},
		{
			name:      "StopFailure writes error",
			session:   "ks-hook-test-stopfail",
			payload:   `{"hook_event_name":"StopFailure","error":"API Error: 529 overloaded","session_id":"s1","transcript_path":"/t","cwd":"/tmp"}`,
			wantState: "error",
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
			name:         "SessionStart from a subagent writes neither state nor record",
			session:      "ks-hook-test-start-agent",
			payload:      `{"hook_event_name":"SessionStart","source":"startup","agent_id":"agent-7","session_id":"sub-1","transcript_path":"/t","cwd":"/tmp"}`,
			seedFile:     state.Entry{State: "working"},
			quiet:        true,
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
			seedFile:     state.Entry{State: "idle"},
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
	if tc.seedFile.State != "" {
		if err := state.WriteEntry(record, tc.seedFile); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("KS_SESSION_NAME", tc.session)
	t.Setenv("KS_SESSION_ID", tc.id)
	useFakeProcessTree(t, tc.nested)

	var stdout, stderr bytes.Buffer
	rootCmd.SetIn(strings.NewReader(tc.payload))
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"_hook"})
	t.Cleanup(func() {
		rootCmd.SetIn(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})

	before := stateFileCount(t)
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want nothing: Claude reads a decision there", stdout.String())
	}
	want := state.Entry{State: tc.wantState, AgentID: tc.wantAgent, Tool: tc.wantTool}
	if tc.quiet {
		want = tc.seedFile
	}
	if tc.seedFile.State != "" && want.State == "" {
		if _, err := state.ReadEntry(record); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("state file for %q still present (err %v), want removed", record, err)
		}
	} else {
		checkState(t, record, want, before, stateFileCount(t))
	}
	if tc.seed != nil {
		checkRecord(t, store, record, tc)
	}
	if tc.quiet && stderr.Len() != 0 {
		t.Errorf("stderr = %q, want nothing", stderr.String())
	}
	if tc.seed == nil && tc.session != "" && !tc.nested && want.State != "" {
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

// checkState asserts the state file for name holds want, stamp aside, or
// that no state file was written when want.State is "".
func checkState(t *testing.T, name string, want state.Entry, before, after int) {
	t.Helper()
	if want.State == "" {
		if after != before {
			t.Fatalf("state dir grew from %d to %d files, want unchanged", before, after)
		}
		if _, err := state.ReadEntry(name); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("state.ReadEntry(%q) err = %v, want not-exist", name, err)
		}
		return
	}
	got, err := state.ReadEntry(name)
	if err != nil {
		t.Fatalf("state.ReadEntry(%q): %v", name, err)
	}
	if !state.IsFresh(got.UpdatedAt) {
		t.Errorf("updated_at %v is not fresh", got.UpdatedAt)
	}
	got.UpdatedAt = time.Time{}
	if got != want {
		t.Errorf("state entry = %+v, want %+v", got, want)
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

// TestAllowWrite pins the policy that keeps an input on the badge until the
// one who asked is done with it.
func TestAllowWrite(t *testing.T) {
	parentBash := state.Entry{State: "input", Tool: "Bash"}
	subBash := state.Entry{State: "input", AgentID: "agent-7", Tool: "Bash"}
	subNoTool := state.Entry{State: "input", AgentID: "agent-7"}
	noTool := state.Entry{State: "input"}
	parentDialog := state.Entry{State: "input", Tool: "mcp__linear__"}
	subDialog := state.Entry{State: "input", AgentID: "agent-7", Tool: "mcp__linear__"}
	working := state.Entry{State: "working"}
	ev := func(event, tool, agent string) hookPayload {
		return hookPayload{HookEventName: event, ToolName: tool, AgentID: agent}
	}
	mcp := func(event, server, agent string) hookPayload {
		return hookPayload{HookEventName: event, MCPServerName: server, AgentID: agent}
	}
	tests := []struct {
		name string
		cur  state.Entry
		p    hookPayload
		s    string
		want bool
	}{
		{
			"parent prompt over a subagent input",
			subBash,
			ev("UserPromptSubmit", "", ""),
			"working",
			true,
		},
		{"parent Stop over a subagent input", subBash, ev("Stop", "", ""), "idle", true},
		{
			"parent StopFailure over its own input",
			parentBash,
			ev("StopFailure", "", ""),
			"error",
			true,
		},
		{
			"parent PreToolUse over a subagent input",
			subBash,
			ev("PreToolUse", "Bash", ""),
			"working",
			true,
		},
		{
			"parent PermissionRequest over its own input",
			parentBash,
			ev("PermissionRequest", "Read", ""),
			"input",
			true,
		},
		{"parent tool done, no input", working, ev("PostToolUse", "Read", ""), "working", true},
		{
			"parent tool done, own input for that tool",
			parentBash,
			ev("PostToolUse", "Bash", ""),
			"working",
			true,
		},
		{
			"parent tool failed, own input for that tool",
			parentBash,
			ev("PostToolUseFailure", "Bash", ""),
			"working",
			true,
		},
		{
			"parent tool done, own input for another tool",
			parentBash,
			ev("PostToolUse", "Read", ""),
			"working",
			false,
		},
		{
			"parent tool done, tool-less input",
			noTool,
			ev("PostToolUse", "Read", ""),
			"working",
			true,
		},
		{
			"parent tool done, subagent input",
			subBash,
			ev("PostToolUse", "Bash", ""),
			"working",
			false,
		},
		{
			"subagent input over a parent input",
			parentBash,
			ev("PermissionRequest", "Read", "agent-7"),
			"input",
			true,
		},
		{
			"subagent question over nothing",
			state.Entry{},
			ev("PreToolUse", "AskUserQuestion", "agent-7"),
			"input",
			true,
		},
		{
			"subagent tool done, its input for that tool",
			subBash,
			ev("PostToolUse", "Bash", "agent-7"),
			"working",
			true,
		},
		{
			"subagent tool failed, its input for that tool",
			subBash,
			ev("PostToolUseFailure", "Bash", "agent-7"),
			"working",
			true,
		},
		{
			"subagent tool done, its input for another tool",
			subBash,
			ev("PostToolUse", "Read", "agent-7"),
			"working",
			false,
		},
		{
			"subagent tool done, its tool-less input",
			subNoTool,
			ev("PostToolUse", "Bash", "agent-7"),
			"working",
			true,
		},
		{
			"subagent tool done, another subagent's input",
			subBash,
			ev("PostToolUse", "Bash", "agent-8"),
			"working",
			false,
		},
		{
			"subagent tool done, parent input",
			parentBash,
			ev("PostToolUse", "Bash", "agent-7"),
			"working",
			false,
		},
		{
			"subagent tool done, no input",
			working,
			ev("PostToolUse", "Bash", "agent-7"),
			"working",
			false,
		},
		{
			"subagent PreToolUse Bash",
			parentBash,
			ev("PreToolUse", "Bash", "agent-7"),
			"working",
			false,
		},
		{"subagent prompt", working, ev("UserPromptSubmit", "", "agent-7"), "working", false},
		{
			"parent dialog answered, its dialog's input",
			parentDialog,
			mcp("ElicitationResult", "linear", ""),
			"working",
			true,
		},
		{
			"parent dialog answered, a Bash input",
			parentBash,
			mcp("ElicitationResult", "linear", ""),
			"working",
			false,
		},
		{
			"parent dialog answered, another server's dialog input",
			parentDialog,
			mcp("ElicitationResult", "github", ""),
			"working",
			false,
		},
		{
			"parent dialog answered, a subagent's dialog input",
			subDialog,
			mcp("ElicitationResult", "linear", ""),
			"working",
			false,
		},
		{
			"parent dialog answered, tool-less input",
			noTool,
			mcp("ElicitationResult", "linear", ""),
			"working",
			true,
		},
		{
			"parent tool done, the server's tool over its dialog input",
			parentDialog,
			ev("PostToolUse", "mcp__linear__create_issue", ""),
			"working",
			true,
		},
		{
			"parent tool done, another tool over its dialog input",
			parentDialog,
			ev("PostToolUse", "Bash", ""),
			"working",
			false,
		},
		{
			"parent tool done, another server's tool over its dialog input",
			parentDialog,
			ev("PostToolUse", "mcp__github__create_issue", ""),
			"working",
			false,
		},
		{
			"subagent dialog over a parent input",
			parentBash,
			mcp("Elicitation", "linear", "agent-7"),
			"input",
			true,
		},
		{
			"subagent dialog answered, its dialog's input",
			subDialog,
			mcp("ElicitationResult", "linear", "agent-7"),
			"working",
			true,
		},
		{
			"subagent dialog answered, the parent's dialog input",
			parentDialog,
			mcp("ElicitationResult", "linear", "agent-7"),
			"working",
			false,
		},
		{
			"subagent tool done, the server's tool over its dialog input",
			subDialog,
			ev("PostToolUse", "mcp__linear__create_issue", "agent-7"),
			"working",
			true,
		},
		{"subagent Stop", working, ev("Stop", "", "agent-7"), "idle", false},
		{"subagent StopFailure", working, ev("StopFailure", "", "agent-7"), "error", false},
		{"subagent SessionStart", working, ev("SessionStart", "", "agent-7"), "waiting", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := allowWrite(tt.cur, tt.p, tt.s); got != tt.want {
				t.Errorf("allowWrite(%+v, %+v, %q) = %v, want %v", tt.cur, tt.p, tt.s, got, tt.want)
			}
		})
	}
}
