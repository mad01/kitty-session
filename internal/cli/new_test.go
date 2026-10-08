package cli

import (
	"strings"
	"testing"

	"github.com/mad01/kitty-session/internal/session"
)

func TestParseKind(t *testing.T) {
	for _, kind := range []string{session.AgentClaude, session.AgentPi, session.AgentShell} {
		if got, err := parseKind(kind); err != nil || got != kind {
			t.Errorf("parseKind(%q) = %q, %v", kind, got, err)
		}
	}
	for _, bad := range []string{"", "codex", "Claude"} {
		if _, err := parseKind(bad); err == nil {
			t.Errorf("parseKind(%q) returned nil", bad)
		}
	}
}

// TestNewAndTmpRejectAnUnknownAgent checks the --agent value is refused
// before anything starts the instance: with a fake HOME and no socket a
// launch attempt would fail with another error than this one.
func TestNewAndTmpRejectAnUnknownAgent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(func() {
		newAgent, tmpAgent = session.AgentClaude, session.AgentClaude
	})
	for _, args := range [][]string{
		{"new", "-n", "x", "--agent", "codex"},
		{"tmp", "--agent", "codex"},
	} {
		out, err := runCmd(t, args...)
		if err == nil || !strings.Contains(err.Error(), `unknown agent "codex"`) {
			t.Errorf("%v: err %v, want the unknown-agent error\noutput: %s", args, err, out)
		}
	}
}
