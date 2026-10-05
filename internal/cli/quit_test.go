package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/mad01/kitty-session/internal/instance"
)

// runCmd executes a ks subcommand against a captured buffer, with the args
// and output streams reset afterwards so the shared rootCmd is clean for the
// next test.
func runCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs(args)
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})
	err := rootCmd.Execute()
	return buf.String(), err
}

func TestQuitWhenInstanceNotRunning(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // no socket here, so Connect reports not running
	out, err := runCmd(t, "quit")
	if err != nil {
		t.Fatalf("quit returned %v, want nil on the not-running path", err)
	}
	if !strings.Contains(out, "ks instance not running") {
		t.Errorf("output = %q, want the not-running message", out)
	}
}

func TestSidebarWhenInstanceNotRunning(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, err := runCmd(t, "sidebar")
	if !errors.Is(err, instance.ErrNotRunning) {
		t.Fatalf("sidebar returned %v, want instance.ErrNotRunning", err)
	}
}
