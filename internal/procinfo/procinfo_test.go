package procinfo

import (
	"errors"
	"os"
	"runtime"
	"testing"
)

func TestOwnProcess(t *testing.T) {
	ppid, err := ParentOf(os.Getpid())
	comm, commErr := CommOf(os.Getpid())

	if runtime.GOOS != "darwin" {
		if !errors.Is(err, ErrUnsupported) || !errors.Is(commErr, ErrUnsupported) {
			t.Fatalf("errors = %v, %v; want ErrUnsupported on %s", err, commErr, runtime.GOOS)
		}
		return
	}
	if err != nil {
		t.Fatalf("ParentOf: %v", err)
	}
	if ppid != os.Getppid() {
		t.Errorf("ParentOf(self) = %d, want %d", ppid, os.Getppid())
	}
	if commErr != nil {
		t.Fatalf("CommOf: %v", commErr)
	}
	if comm == "" {
		t.Error("CommOf(self) is empty")
	}
}

func TestMissingProcess(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("no process lookup on " + runtime.GOOS)
	}
	// PIDs are 32-bit on darwin; this one is far past pid_max.
	const missing = 1 << 30
	if _, err := ParentOf(missing); err == nil {
		t.Error("ParentOf(missing) = nil error, want failure")
	}
	if _, err := CommOf(missing); err == nil {
		t.Error("CommOf(missing) = nil error, want failure")
	}
}
