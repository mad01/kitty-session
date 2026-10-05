package cli

import (
	"errors"
	"fmt"
	"testing"

	"github.com/mad01/kitty-session/internal/procinfo"
)

// fakeProc is one process in a fake tree.
type fakeProc struct {
	ppid int
	comm string
}

// fakeTree builds a processLookup over a pid → process map; unknown pids fail
// like a lookup of an exited process would.
func fakeTree(procs map[int]fakeProc) processLookup {
	return processLookup{
		parentOf: func(pid int) (int, error) {
			p, ok := procs[pid]
			if !ok {
				return 0, fmt.Errorf("no such pid %d", pid)
			}
			return p.ppid, nil
		},
		commOf: func(pid int) (string, error) {
			p, ok := procs[pid]
			if !ok {
				return "", fmt.Errorf("no such pid %d", pid)
			}
			return p.comm, nil
		},
	}
}

// Process ids in the fake tree, named for what they are.
const (
	pidKitty       = 10
	pidClaude      = 100 // the one ks launched: a child of kitty
	pidHookSh      = 200 // sh wrapper Claude Code may run a hook through
	pidBashTool    = 250 // a Bash tool shell inside the session
	pidNested      = 300 // `claude -p` run from that shell
	pidNestedSh    = 350
	pidHerdr       = 40
	pidHerdrShell  = 50
	pidHerdrClaude = 60
)

// Treat the fake tree as the hook's view: the hook's parent is the pid under test.
func TestFiredByTopLevelClaude(t *testing.T) {
	tree := fakeTree(map[int]fakeProc{
		pidKitty:       {ppid: 1, comm: "kitty"},
		pidClaude:      {ppid: pidKitty, comm: "claude"},
		pidHookSh:      {ppid: pidClaude, comm: "sh"},
		pidBashTool:    {ppid: pidClaude, comm: "zsh"},
		pidNested:      {ppid: pidBashTool, comm: "claude"},
		pidNestedSh:    {ppid: pidNested, comm: "bash"},
		pidHerdr:       {ppid: 1, comm: "herdr"},
		pidHerdrShell:  {ppid: pidHerdr, comm: "zsh"},
		pidHerdrClaude: {ppid: pidHerdrShell, comm: "claude"},
	})
	tests := []struct {
		name    string
		parent  int
		want    bool
		wantErr bool
	}{
		{"claude directly under kitty", pidClaude, true, false},
		{"claude under kitty, hook via sh", pidHookSh, true, false},
		{"claude nested in a Bash tool shell", pidNested, false, false},
		{"nested claude, hook via bash", pidNestedSh, false, false},
		{"claude under another multiplexer", pidHerdrClaude, false, false},
		{"unknown parent pid", 999, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := firedByTopLevelClaude(tree, tc.parent)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("top-level = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFiredByTopLevelClaudeUnsupported(t *testing.T) {
	unsupported := processLookup{
		parentOf: func(int) (int, error) { return 0, procinfo.ErrUnsupported },
		commOf:   func(int) (string, error) { return "", procinfo.ErrUnsupported },
	}
	_, err := firedByTopLevelClaude(unsupported, pidClaude)
	if !errors.Is(err, procinfo.ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported passed through", err)
	}
}
