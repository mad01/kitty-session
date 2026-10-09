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

// piHookCase is one pi row of TestHook, which runs these after the claude
// cases so both share the one fake HOME state.Dir pins. The fields mirror
// hookCase: session is KS_SESSION_NAME, record the name the seed is saved
// under when that differs, as after a rename.
type piHookCase struct {
	name    string
	session string
	id      string // KS_SESSION_ID
	payload string
	// wantState is the state expected under the resolved record's name; ""
	// means no state file may be written.
	wantState string
	seedState string // state file written before the hook runs; must be gone after
	seed      *session.Session
	record    string
	// wantWarn says a missing-record line is expected on stderr: every event ks
	// acts on reports it; an ignored event or an empty environment never looks.
	wantWarn bool
	wantStatus,
	wantPiID,
	wantPiPath string
}

func piHookCases() []piHookCase {
	return []piHookCase{
		{
			name:      "pi agent_start writes working",
			session:   "ks-pihook-test-start",
			wantWarn:  true,
			payload:   `{"event":"agent_start","session_id":"p1","session_file":"/s.jsonl"}`,
			wantState: "working",
		},
		{
			name:      "pi tool_call writes working",
			session:   "ks-pihook-test-tool",
			wantWarn:  true,
			payload:   `{"event":"tool_call","session_id":"p1","session_file":"/s.jsonl"}`,
			wantState: "working",
		},
		{
			name:      "pi agent_settled writes idle",
			session:   "ks-pihook-test-settled",
			wantWarn:  true,
			payload:   `{"event":"agent_settled","session_id":"p1","session_file":"/s.jsonl"}`,
			wantState: "idle",
		},
		{
			name:      "pi blocked writes input",
			session:   "ks-pihook-test-blocked",
			wantWarn:  true,
			payload:   `{"event":"blocked","label":"bash: rm -rf build"}`,
			wantState: "input",
		},
		{
			name:      "pi unblocked writes working",
			session:   "ks-pihook-test-unblocked",
			wantWarn:  true,
			payload:   `{"event":"unblocked"}`,
			wantState: "working",
		},
		{
			name:      "pi session_start without a session record still writes waiting",
			session:   "ks-pihook-test-session-start",
			wantWarn:  true,
			payload:   `{"event":"session_start","reason":"startup","session_id":"p1","session_file":"/s.jsonl"}`,
			wantState: "waiting",
		},
		{
			name:       "pi session_start records id and file and reactivates",
			session:    "ks-pihook-test-session-record",
			payload:    `{"event":"session_start","reason":"resume","session_id":"abc-123","session_file":"/home/u/.pi/agent/sessions/--tmp--/x_abc-123.jsonl"}`,
			wantState:  "waiting",
			seed:       &session.Session{Agent: session.AgentPi, Status: session.StatusStopped, PiSessionID: "old"},
			wantStatus: session.StatusActive,
			wantPiID:   "abc-123",
			wantPiPath: "/home/u/.pi/agent/sessions/--tmp--/x_abc-123.jsonl",
		},
		{
			name:       "pi session_start with empty session id leaves the record alone",
			session:    "ks-pihook-test-session-empty",
			payload:    `{"event":"session_start","reason":"startup","session_id":"","session_file":""}`,
			wantState:  "waiting",
			seed:       &session.Session{Agent: session.AgentPi, Status: session.StatusStopped, PiSessionID: "keep", PiSessionPath: "/keep"},
			wantStatus: session.StatusStopped,
			wantPiID:   "keep",
			wantPiPath: "/keep",
		},
		{
			name:       "pi KS_SESSION_ID finds a renamed record and keys state by its new name",
			session:    "ks-pihook-test-old-name",
			id:         "pi-id-renamed",
			payload:    `{"event":"session_start","reason":"startup","session_id":"new-1","session_file":"/n.jsonl"}`,
			wantState:  "waiting",
			seed:       &session.Session{ID: "pi-id-renamed", Agent: session.AgentPi, Status: session.StatusStopped},
			record:     "ks-pihook-test-new-name",
			wantStatus: session.StatusActive,
			wantPiID:   "new-1",
			wantPiPath: "/n.jsonl",
		},
		{
			name:       "pi session_shutdown quit drops the state file and keeps the record active",
			session:    "ks-pihook-test-quit",
			payload:    `{"event":"session_shutdown","reason":"quit","session_id":"p1","session_file":"/s.jsonl"}`,
			seedState:  "idle",
			seed:       &session.Session{Agent: session.AgentPi, Status: session.StatusActive, PiSessionID: "p1", PiSessionPath: "/s.jsonl"},
			wantStatus: session.StatusActive,
			wantPiID:   "p1",
			wantPiPath: "/s.jsonl",
		},
		{
			name:       "pi session_shutdown reload drops the state file and keeps the record active",
			session:    "ks-pihook-test-reload",
			payload:    `{"event":"session_shutdown","reason":"reload"}`,
			seedState:  "working",
			seed:       &session.Session{Agent: session.AgentPi, Status: session.StatusActive},
			wantStatus: session.StatusActive,
		},
		{
			name:     "pi session_shutdown without a session record is silent",
			session:  "ks-pihook-test-quit-missing",
			wantWarn: true,
			payload:  `{"event":"session_shutdown","reason":"quit"}`,
		},
		{
			name:    "pi unknown event writes nothing",
			session: "ks-pihook-test-unknown",
			payload: `{"event":"turn_end","session_id":"p1"}`,
		},
		{
			name:     "pi KS_SESSION_ID without a record and no name writes nothing",
			session:  "",
			id:       "pi-id-nobody-has",
			wantWarn: true,
			payload:  `{"event":"agent_start"}`,
		},
		{
			name:    "pi empty environment writes nothing",
			session: "",
			payload: `{"event":"agent_start","session_id":"p1"}`,
		},
	}
}

func runPiHookCase(t *testing.T, store *session.Store, tc piHookCase) {
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

	var stderr bytes.Buffer
	rootCmd.SetIn(strings.NewReader(tc.payload))
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"_pi-hook"})
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
	} else {
		checkState(t, record, tc.wantState, before, stateFileCount(t))
	}
	if tc.seed != nil {
		checkPiRecord(t, store, record, tc)
	}
	if got := strings.Contains(stderr.String(), "ks _pi-hook:"); got != tc.wantWarn {
		t.Errorf("stderr = %q, want a ks _pi-hook: line = %v", stderr.String(), tc.wantWarn)
	}
}

func checkPiRecord(t *testing.T, store *session.Store, name string, tc piHookCase) {
	t.Helper()
	got, err := store.Load(name)
	if err != nil {
		t.Fatalf("Load(%q): %v", name, err)
	}
	if got.Status != tc.wantStatus {
		t.Errorf("Status = %q, want %q", got.Status, tc.wantStatus)
	}
	if got.PiSessionID != tc.wantPiID {
		t.Errorf("PiSessionID = %q, want %q", got.PiSessionID, tc.wantPiID)
	}
	if got.PiSessionPath != tc.wantPiPath {
		t.Errorf("PiSessionPath = %q, want %q", got.PiSessionPath, tc.wantPiPath)
	}
	if got.Kind() != session.AgentPi {
		t.Errorf("Kind() = %q, want %q: the hook must not touch the agent kind", got.Kind(), session.AgentPi)
	}
}
