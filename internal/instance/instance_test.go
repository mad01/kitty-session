package instance

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mad01/kitty-session/internal/kitty"
)

// fakeKitty answers Ping from a script: it fails untilUp times, then
// succeeds, unless Start was never called (an instance that is not started
// never comes up).
type fakeKitty struct {
	untilUp  int // Pings to fail after Start before answering
	startErr error

	pings   int
	started []kitty.StartOptions
	slept   time.Duration
}

func (f *fakeKitty) Ping() error {
	f.pings++
	if len(f.started) == 0 {
		return errors.New("connection refused")
	}
	if f.pings-1 < f.untilUp { // pings before Start count too; see test setup
		return errors.New("connection refused")
	}
	return nil
}

func (f *fakeKitty) Start(o kitty.StartOptions) error {
	f.started = append(f.started, o)
	return f.startErr
}

func (f *fakeKitty) sleep(d time.Duration) { f.slept += d }

func newBoot(t *testing.T, f *fakeKitty, timeout time.Duration) (boot, string) {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "kitty.sock")
	return boot{
		socketPath: sock,
		start:      kitty.StartOptions{Command: []string{"/bin/ks", "sidebar"}, Title: "ks"},
		sleep:      f.sleep,
		timeout:    timeout,
	}, sock
}

func TestEnsureLeavesARunningInstanceAlone(t *testing.T) {
	running := &alwaysUp{}
	b, sock := newBoot(t, &fakeKitty{}, time.Second)
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensure(running, b); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if running.started != 0 {
		t.Errorf("Start called %d times for a running instance", running.started)
	}
	if _, err := os.Stat(sock); err != nil {
		t.Errorf("live socket file removed: %v", err)
	}
}

// alwaysUp is an instance that answers every Ping.
type alwaysUp struct{ started int }

func (a *alwaysUp) Ping() error                    { return nil }
func (a *alwaysUp) Start(kitty.StartOptions) error { a.started++; return nil }

func TestEnsureStartsAndWaitsForTheSocket(t *testing.T) {
	// One failing Ping before Start, then three more while kitty boots.
	f := &fakeKitty{untilUp: 4}
	b, sock := newBoot(t, f, time.Second)
	if err := os.WriteFile(sock, nil, 0o600); err != nil { // stale file from a dead instance
		t.Fatal(err)
	}

	if err := ensure(f, b); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if len(f.started) != 1 {
		t.Fatalf("Start called %d times, want 1", len(f.started))
	}
	if got := f.started[0].Command; len(got) != 2 || got[1] != "sidebar" {
		t.Errorf("Start command = %q, want the home sidebar", got)
	}
	if _, err := os.Stat(sock); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stale socket not removed before Start (stat err %v)", err)
	}
	if f.pings != 5 { // 1 before Start + 4 polls
		t.Errorf("pings = %d, want 5", f.pings)
	}
	if f.slept != 4*startPoll {
		t.Errorf("slept %v, want %v", f.slept, 4*startPoll)
	}
}

func TestEnsureGivesUpAfterTheTimeout(t *testing.T) {
	f := &fakeKitty{untilUp: 1 << 20} // never answers
	b, _ := newBoot(t, f, 10*startPoll)

	err := ensure(f, b)
	if err == nil || !strings.Contains(err.Error(), "did not answer") {
		t.Fatalf("err = %v, want a timeout", err)
	}
	if f.slept != 10*startPoll {
		t.Errorf("slept %v, want the full timeout %v", f.slept, 10*startPoll)
	}
}

func TestEnsureReportsAStartFailure(t *testing.T) {
	f := &fakeKitty{startErr: errors.New("kitty: command not found")}
	b, _ := newBoot(t, f, time.Second)

	err := ensure(f, b)
	if err == nil || !strings.Contains(err.Error(), "cannot start ks instance") ||
		!strings.Contains(err.Error(), "command not found") {
		t.Fatalf("err = %v, want the start failure wrapped", err)
	}
	if f.slept != 0 {
		t.Errorf("waited %v after a failed Start", f.slept)
	}
}
