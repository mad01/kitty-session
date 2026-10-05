package cli

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// TestStopAgentKillsTheProcessGroup starts a real child in its own process
// group, as startAgent does, and checks stopAgent reaps it.
func TestStopAgentKillsTheProcessGroup(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep not on PATH")
	}
	cmd := exec.Command(sleep, "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	pid := cmd.Process.Pid

	stopAgent(cmd)

	// stopAgent waits on the child, so it is reaped by the time it returns;
	// signalling pid 0 then reports no such process.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			return // gone
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("child %d still alive after stopAgent", pid)
}

// TestStopAgentNilIsNoOp guards the paths where the agent never started.
func TestStopAgentNilIsNoOp(t *testing.T) {
	stopAgent(nil)
	stopAgent(&exec.Cmd{})
}
