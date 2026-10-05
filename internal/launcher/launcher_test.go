package launcher

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/mad01/kitty-session/internal/repo/config"
	"github.com/mad01/kitty-session/internal/session"
)

// fakeBackend records the launch sequence instead of driving kitty.
type fakeBackend struct {
	tabAlive   bool
	summaryErr error
	focusErr   error

	calls      []string // method names in call order
	launchArgs []string // args passed to LaunchTab
}

const (
	fakeClaudeWindow  = 100
	fakeTab           = 7
	fakeShellWindow   = 101
	fakeSummaryWindow = 102
)

func (f *fakeBackend) record(name string) { f.calls = append(f.calls, name) }

func (f *fakeBackend) TabExists(int) bool { f.record("TabExists"); return f.tabAlive }
func (f *fakeBackend) FocusTab(int) error { f.record("FocusTab"); return nil }

func (f *fakeBackend) FocusWindow(int) error {
	f.record("FocusWindow")
	return f.focusErr
}

func (f *fakeBackend) LaunchTab(_ string, args ...string) (int, error) {
	f.record("LaunchTab")
	f.launchArgs = args
	return fakeClaudeWindow, nil
}

func (f *fakeBackend) SetTabTitle(string) error { f.record("SetTabTitle"); return nil }

func (f *fakeBackend) FindTabForWindow(int) (int, error) {
	f.record("FindTabForWindow")
	return fakeTab, nil
}

func (f *fakeBackend) LaunchTabInWindow(int, string, ...string) (int, error) {
	f.record("LaunchTabInWindow")
	return fakeShellWindow, nil
}

func (f *fakeBackend) LaunchSplit(string, ...string) error {
	f.record("LaunchSplit")
	return nil
}

func (f *fakeBackend) LaunchSummary(int, int, string) (int, error) {
	f.record("LaunchSummary")
	if f.summaryErr != nil {
		return 0, f.summaryErr
	}
	return fakeSummaryWindow, nil
}

func TestClaudeArgs(t *testing.T) {
	t.Setenv("PATH", "/opt/claude/bin:/usr/bin")
	tests := []struct {
		name     string
		mode     ResumeMode
		claudeID string
		wantTail []string // everything after "claude"
	}{
		{"new session starts claude bare", ResumeNone, "", nil},
		{"new session ignores a stale id", ResumeNone, "abc", nil},
		{"reopen with id resumes it", ResumeStored, "abc", []string{"--resume", "abc"}},
		{"reopen without id continues", ResumeStored, "", []string{"--continue"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			args := claudeArgs("my-session", tc.mode, tc.claudeID)

			i := slices.Index(args, "claude")
			if i < 1 || args[i-1] != "--" {
				t.Fatalf("args %q: want `-- claude`", args)
			}
			if tail := args[i+1:]; !slices.Equal(tail, tc.wantTail) {
				t.Errorf("claude args = %q, want %q", tail, tc.wantTail)
			}
			wantEnv := []string{
				"PATH=/opt/claude/bin:/usr/bin",
				"KS_SESSION_NAME=my-session",
			}
			for _, env := range wantEnv {
				if !hasEnv(args, env) {
					t.Errorf("args %q: missing --env %s", args, env)
				}
			}
		})
	}
}

func hasEnv(args []string, want string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--env" && args[i+1] == want {
			return true
		}
	}
	return false
}

func TestOpenNewSession(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *config.Config
		summaryErr  error
		focusErr    error
		wantCalls   []string
		wantShell   int
		wantSummary int
		wantWarns   int
	}{
		{
			name: "split layout, no config",
			cfg:  nil,
			wantCalls: []string{
				"LaunchTab", "SetTabTitle", "FindTabForWindow", "LaunchSplit", "FocusWindow",
			},
		},
		{
			name: "tab layout with summary",
			cfg:  &config.Config{Layout: config.LayoutTab, Summary: true},
			wantCalls: []string{
				"LaunchTab", "SetTabTitle", "FindTabForWindow", "LaunchTabInWindow",
				"LaunchSummary", "FocusWindow",
			},
			wantShell:   fakeShellWindow,
			wantSummary: fakeSummaryWindow,
		},
		{
			name:       "summary and focus failures are warnings",
			cfg:        &config.Config{Layout: config.LayoutTab, Summary: true},
			summaryErr: errors.New("haiku down"),
			focusErr:   errors.New("no focus"),
			wantCalls: []string{
				"LaunchTab", "SetTabTitle", "FindTabForWindow", "LaunchTabInWindow",
				"LaunchSummary", "FocusWindow",
			},
			wantShell: fakeShellWindow,
			wantWarns: 2,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := newTestStore(t)
			b := &fakeBackend{summaryErr: tc.summaryErr, focusErr: tc.focusErr}

			res, err := open(store, tc.cfg, b, Request{Name: "demo", Dir: "/work/demo"})
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			if !slices.Equal(b.calls, tc.wantCalls) {
				t.Errorf("calls = %v, want %v", b.calls, tc.wantCalls)
			}
			if len(res.Warnings) != tc.wantWarns {
				t.Errorf("warnings = %v, want %d", res.Warnings, tc.wantWarns)
			}
			if res.Focused {
				t.Error("Focused = true for a new session")
			}
			if hasArg(b.launchArgs, "--continue") || hasArg(b.launchArgs, "--resume") {
				t.Errorf("new session launched with resume flags: %q", b.launchArgs)
			}

			got, err := store.Load("demo")
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			checkIDs(t, got, fakeTab, fakeClaudeWindow, tc.wantShell, tc.wantSummary)
			if got.Status != session.StatusActive {
				t.Errorf("Status = %q, want active", got.Status)
			}
			if got.Dir != "/work/demo" {
				t.Errorf("Dir = %q, want /work/demo", got.Dir)
			}
		})
	}
}

func TestOpenStoredSession(t *testing.T) {
	tests := []struct {
		name        string
		stored      session.Session
		tabAlive    bool
		wantFocused bool
		wantResume  []string // trailing claude args; nil when not launched
	}{
		{
			name: "live tab is focused, nothing launched",
			stored: session.Session{
				KittyTabID:    3,
				KittyWindowID: 9,
				Status:        session.StatusActive,
			},
			tabAlive:    true,
			wantFocused: true,
		},
		{
			name: "dead tab with claude id resumes by id",
			stored: session.Session{
				KittyTabID: 3, KittyShellWindowID: 5, KittySummaryWindowID: 6,
				Status: session.StatusStopped, ClaudeSessionID: "uuid-1",
			},
			wantResume: []string{"--resume", "uuid-1"},
		},
		{
			name:       "dead tab without claude id continues",
			stored:     session.Session{KittyTabID: 3},
			wantResume: []string{"--continue"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := newTestStore(t)
			tc.stored.Name = "demo"
			tc.stored.Dir = "/work/demo"
			if err := store.Save(&tc.stored); err != nil {
				t.Fatal(err)
			}
			b := &fakeBackend{tabAlive: tc.tabAlive}

			res, err := open(store, nil, b, Request{Name: "demo", Resume: ResumeStored})
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			if res.Focused != tc.wantFocused {
				t.Fatalf("Focused = %v, want %v (calls %v)", res.Focused, tc.wantFocused, b.calls)
			}
			if tc.wantFocused {
				if slices.Contains(b.calls, "LaunchTab") {
					t.Errorf("focused session relaunched: %v", b.calls)
				}
				return
			}

			i := slices.Index(b.launchArgs, "claude")
			if tail := b.launchArgs[i+1:]; !slices.Equal(tail, tc.wantResume) {
				t.Errorf("claude args = %q, want %q", tail, tc.wantResume)
			}
			got, err := store.Load("demo")
			if err != nil {
				t.Fatal(err)
			}
			// Split layout, no summary: stale shell/summary IDs must be cleared.
			checkIDs(t, got, fakeTab, fakeClaudeWindow, 0, 0)
			if got.Status != session.StatusActive {
				t.Errorf("Status = %q, want active after reopen", got.Status)
			}
			if got.ClaudeSessionID != tc.stored.ClaudeSessionID {
				t.Errorf(
					"ClaudeSessionID = %q, want %q kept",
					got.ClaudeSessionID,
					tc.stored.ClaudeSessionID,
				)
			}
		})
	}
}

func TestOpenStoredSessionMissing(t *testing.T) {
	store := newTestStore(t)
	_, err := open(store, nil, &fakeBackend{}, Request{Name: "ghost", Resume: ResumeStored})
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("err = %v, want not-found naming ghost", err)
	}
}

func newTestStore(t *testing.T) *session.Store {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	store, err := session.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func hasArg(args []string, want string) bool { return slices.Contains(args, want) }

func checkIDs(t *testing.T, got *session.Session, tab, claude, shell, summary int) {
	t.Helper()
	if got.KittyTabID != tab {
		t.Errorf("KittyTabID = %d, want %d", got.KittyTabID, tab)
	}
	if got.KittyWindowID != claude {
		t.Errorf("KittyWindowID = %d, want %d", got.KittyWindowID, claude)
	}
	if got.KittyShellWindowID != shell {
		t.Errorf("KittyShellWindowID = %d, want %d", got.KittyShellWindowID, shell)
	}
	if got.KittySummaryWindowID != summary {
		t.Errorf("KittySummaryWindowID = %d, want %d", got.KittySummaryWindowID, summary)
	}
}
