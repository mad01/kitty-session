package instance

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mad01/kitty-session/internal/kitty"
)

// fakeKitty answers Ping from a script: it fails untilUp times, then
// succeeds, unless Start was never called (an instance that is not started
// never comes up). Once started it holds the one window Start launched.
type fakeKitty struct {
	untilUp    int // Pings to fail after Start before answering
	startErr   error
	windowsErr error
	varsErr    error

	pings   int
	started []kitty.StartOptions
	slept   time.Duration
	tagged  []string // "<window id>:<vars>" per SetUserVars call
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

func (f *fakeKitty) Windows() ([]kitty.Window, error) {
	if f.windowsErr != nil {
		return nil, f.windowsErr
	}
	if len(f.started) == 0 {
		return nil, nil
	}
	return []kitty.Window{{ID: 1, TabID: 1, Title: "ks"}}, nil
}

func (f *fakeKitty) SetUserVars(id int, vars ...string) error {
	f.tagged = append(f.tagged, fmt.Sprintf("%d:%s", id, strings.Join(vars, ",")))
	return f.varsErr
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
	started, err := ensure(running, b)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if started {
		t.Error("ensure reported a start for a running instance")
	}
	if running.started != 0 {
		t.Errorf("Start called %d times for a running instance", running.started)
	}
	if running.tagged != 0 {
		t.Errorf("SetUserVars called %d times for a running instance", running.tagged)
	}
	if _, err := os.Stat(sock); err != nil {
		t.Errorf("live socket file removed: %v", err)
	}
}

// alwaysUp is an instance that answers every Ping.
type alwaysUp struct{ started, tagged int }

func (a *alwaysUp) Ping() error                        { return nil }
func (a *alwaysUp) Start(kitty.StartOptions) error     { a.started++; return nil }
func (a *alwaysUp) Windows() ([]kitty.Window, error)   { return nil, nil }
func (a *alwaysUp) SetUserVars(int, ...string) error   { a.tagged++; return nil }

func TestEnsureStartsAndWaitsForTheSocket(t *testing.T) {
	// One failing Ping before Start, then three more while kitty boots.
	f := &fakeKitty{untilUp: 4}
	b, sock := newBoot(t, f, time.Second)
	if err := os.WriteFile(sock, nil, 0o600); err != nil { // stale file from a dead instance
		t.Fatal(err)
	}

	started, err := ensure(f, b)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if !started {
		t.Error("ensure did not report the start")
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
	if f.pings != 5 { // 1 before Start + 1 re-ping after unlink + 3 polls
		t.Errorf("pings = %d, want 5", f.pings)
	}
	if f.slept != 3*startPoll {
		t.Errorf("slept %v, want %v", f.slept, 3*startPoll)
	}
	// The first window is the home sidebar; it is tagged before ensure returns.
	if want := []string{"1:KS_HOME=1"}; !slices.Equal(f.tagged, want) {
		t.Errorf("tagged = %q, want %q", f.tagged, want)
	}
}

func TestEnsureReportsAHomeTagFailure(t *testing.T) {
	tests := []struct {
		name string
		fake *fakeKitty
		want string
	}{
		{
			name: "a snapshot that fails",
			fake: &fakeKitty{windowsErr: errors.New("ls broke")},
			want: "cannot list its windows",
		},
		{
			name: "a tag that fails",
			fake: &fakeKitty{varsErr: errors.New("set-user-vars broke")},
			want: "cannot tag its home tab",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, _ := newBoot(t, tc.fake, time.Second)
			started, err := ensure(tc.fake, b)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
			if started {
				t.Error("ensure reported a start despite the error")
			}
			if len(tc.fake.started) != 1 {
				t.Errorf("Start called %d times, want 1", len(tc.fake.started))
			}
		})
	}
}

func TestEnsureKeepsAWedgedSocket(t *testing.T) {
	// A socket that is not stale (a live or wedged listener) must never be
	// unlinked, even when the first Ping fails.
	f := &fakeKitty{untilUp: 0}
	b, sock := newBoot(t, f, time.Second)
	b.stale = func(string) bool { return false }
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ensure(f, b); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if _, err := os.Stat(sock); err != nil {
		t.Errorf("non-stale socket removed: %v", err)
	}
	if len(f.started) != 1 {
		t.Errorf("Start called %d times, want 1", len(f.started))
	}
}

func TestEnsureGivesUpAfterTheTimeout(t *testing.T) {
	f := &fakeKitty{untilUp: 1 << 20} // never answers
	b, _ := newBoot(t, f, 10*startPoll)

	_, err := ensure(f, b)
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

	_, err := ensure(f, b)
	if err == nil || !strings.Contains(err.Error(), "cannot start ks instance") ||
		!strings.Contains(err.Error(), "command not found") {
		t.Fatalf("err = %v, want the start failure wrapped", err)
	}
	if f.slept != 0 {
		t.Errorf("waited %v after a failed Start", f.slept)
	}
}
