package cli

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mad01/kitty-session/internal/kitty"
	"github.com/mad01/kitty-session/internal/session"
)

// TestListShowsTheKind runs ks list against a store under a fake HOME with
// no instance, so every row is stopped and the kind column is what varies.
func TestListShowsTheKind(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store, err := session.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	old := session.New("ks-list-old", "/work/old", 0, 0) // no agent field: claude
	pi := session.New("ks-list-pi", "/work/pi", 0, 0)
	pi.Agent = session.AgentPi
	sh := session.New("ks-list-sh", "/work/sh", 0, 0)
	sh.Agent = session.AgentShell
	for _, s := range []*session.Session{old, pi, sh} {
		if err := store.Save(s); err != nil {
			t.Fatal(err)
		}
	}

	out, err := runCmd(t, "list")
	if err != nil {
		t.Fatalf("list returned %v\noutput: %s", err, out)
	}
	for _, want := range []string{
		"ks-list-old          claude  stopped    /work/old",
		"ks-list-pi           pi      stopped    /work/pi",
		"ks-list-sh           shell   stopped    /work/sh",
		"ks instance not running",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

// TestListStateReadsTheProgramStatus feeds listState fake snapshots: a
// window with a Program Status record prints the record's state whatever
// the kind unless a fresh state file outranks it, and without one each kind
// keeps its own rules. The state file comes from a fake reader, since
// state.Dir pins one HOME per process, and the terminal text from a fake
// too, so the kitty client is nil.
func TestListStateReadsTheProgramStatus(t *testing.T) {
	var file string      // the fake state file's state, "" for none
	var fileAt time.Time // its stamp
	var text string      // the fake terminal text
	prevRead, prevText := readState, paneText
	readState = func(string) (string, time.Time, error) {
		if file == "" {
			return "", time.Time{}, errors.New("no state file")
		}
		return file, fileAt, nil
	}
	paneText = func(*kitty.Client, int) (string, error) { return text, nil }
	t.Cleanup(func() { readState, paneText = prevRead, prevText })
	fresh, stale := time.Now(), time.Now().Add(-time.Minute)
	tests := []struct {
		name   string
		kind   string
		status kitty.ProgramState
		file   string // the state file's state, "" for none
		fileAt time.Time
		active bool   // the window's tab is the one on screen
		text   string // the window's terminal text, for a claude row past its state file
		want   string
	}{
		{
			name:   "claude blocked",
			kind:   session.AgentClaude,
			status: kitty.ProgramBlocked,
			want:   "input",
		},
		{
			name:   "claude working",
			kind:   session.AgentClaude,
			status: kitty.ProgramWorking,
			want:   "working",
		},
		{name: "claude done", kind: session.AgentClaude, status: kitty.ProgramDone, want: "done"},
		{
			name:   "claude error",
			kind:   session.AgentClaude,
			status: kitty.ProgramError,
			want:   "error",
		},
		{name: "claude idle", kind: session.AgentClaude, status: kitty.ProgramIdle, want: "idle"},
		{
			name: "done on the active tab is idle", kind: session.AgentClaude,
			status: kitty.ProgramDone, active: true, want: "idle",
		},
		{
			name: "a fresh working file outranks a blocked record", kind: session.AgentClaude,
			status: kitty.ProgramBlocked, file: "working", fileAt: fresh, want: "working",
		},
		{
			name: "a blocked record outranks a stale working file", kind: session.AgentClaude,
			status: kitty.ProgramBlocked, file: "working", fileAt: stale, want: "input",
		},
		{
			name: "pi blocked over a stale idle file", kind: session.AgentPi,
			status: kitty.ProgramBlocked, file: "idle", fileAt: stale, want: "input",
		},
		{
			name: "pi: a fresh idle file outranks a blocked record", kind: session.AgentPi,
			status: kitty.ProgramBlocked, file: "idle", fileAt: fresh, want: "idle",
		},
		{
			name:   "shell working",
			kind:   session.AgentShell,
			status: kitty.ProgramWorking,
			want:   "working",
		},
		{
			name: "no record: shell is idle", kind: session.AgentShell,
			file: "input", fileAt: fresh, want: "idle",
		},
		{
			name: "no record: pi reads its stale state file", kind: session.AgentPi,
			file: "working", fileAt: stale, want: "working",
		},
		{
			name: "no record: claude reads a fresh state file", kind: session.AgentClaude,
			file: "input", fileAt: fresh, want: "input",
		},
		{
			name: "no record: claude reads a fresh error file", kind: session.AgentClaude,
			file: "error", fileAt: fresh, want: "error",
		},
		{
			name: "no record: a stale error file beats idle pane text", kind: session.AgentClaude,
			file: "error", fileAt: stale, text: "API Error: 529 overloaded\n>", want: "error",
		},
		{
			name: "no record: working pane text beats a stale error file", kind: session.AgentClaude,
			file: "error", fileAt: stale, text: "⠋ Reading internal/cli/list.go", want: "working",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file, fileAt, text = tt.file, tt.fileAt, tt.text
			sess := session.New("ks-list", "/work", 1, 7)
			sess.Agent = tt.kind
			all := []kitty.Window{{
				ID: 7, TabID: 1, TabActive: tt.active, SessionID: sess.ID, Status: tt.status,
			}}
			if got := listState(nil, all, sess); got != tt.want {
				t.Errorf("listState = %q, want %q", got, tt.want)
			}
		})
	}
	file = ""
	t.Run("stopped record ignores the record", func(t *testing.T) {
		sess := session.New("ks-list-stopped", "/work", 1, 7)
		sess.Status = session.StatusStopped
		all := []kitty.Window{{ID: 7, TabID: 1, SessionID: sess.ID, Status: kitty.ProgramBlocked}}
		if got := listState(nil, all, sess); got != "stopped" {
			t.Errorf("listState = %q, want stopped", got)
		}
	})
	t.Run("window gone ignores the record", func(t *testing.T) {
		sess := session.New("ks-list-gone", "/work", 1, 7)
		all := []kitty.Window{{
			ID: 7, TabID: 1, SessionID: "someone-else", Status: kitty.ProgramBlocked,
		}}
		if got := listState(nil, all, sess); got != "stopped" {
			t.Errorf("listState = %q, want stopped", got)
		}
	})
}
