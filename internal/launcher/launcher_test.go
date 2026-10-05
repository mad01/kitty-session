package launcher

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mad01/kitty-session/internal/claude"
	"github.com/mad01/kitty-session/internal/repo/config"
	"github.com/mad01/kitty-session/internal/session"
)

// fakeBackend records the launch sequence instead of driving kitty.
type fakeBackend struct {
	tabAlive    bool
	windowAlive bool // the claude window (and any shell/summary window) is in kitty @ ls
	summaryErr  error
	focusErr    error
	closeErr    error
	onLaunch    func() // runs inside LaunchTab, standing in for the SessionStart hook

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

func (f *fakeBackend) TabExists(int) bool    { f.record("TabExists"); return f.tabAlive }
func (f *fakeBackend) WindowExists(int) bool { f.record("WindowExists"); return f.windowAlive }
func (f *fakeBackend) CloseTab(int) error    { f.record("CloseTab"); return f.closeErr }
func (f *fakeBackend) FocusTab(int) error    { f.record("FocusTab"); return nil }

func (f *fakeBackend) CloseTabForWindow(int) error {
	f.record("CloseTabForWindow")
	return f.closeErr
}

func (f *fakeBackend) FocusWindow(int) error {
	f.record("FocusWindow")
	return f.focusErr
}

func (f *fakeBackend) LaunchTab(_ string, args ...string) (int, error) {
	f.record("LaunchTab")
	f.launchArgs = args
	if f.onLaunch != nil {
		f.onLaunch()
	}
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

// splitLaunch is the call sequence of a split-layout launch without summary.
var splitLaunch = []string{
	"LaunchTab", "SetTabTitle", "FindTabForWindow", "LaunchSplit", "FocusWindow",
}

func TestClaudeArgs(t *testing.T) {
	t.Setenv("PATH", "/opt/claude/bin:/usr/bin")
	home := t.TempDir()
	t.Setenv("HOME", home)
	derived, err := claude.TranscriptPath("/work/demo", "derived-id")
	if err != nil {
		t.Fatal(err)
	}
	touch(t, derived)
	stored := filepath.Join(home, "elsewhere", "stored-id.jsonl")
	touch(t, stored)

	tests := []struct {
		name     string
		mode     ResumeMode
		sess     session.Session
		wantTail []string // everything after "claude"
	}{
		{"new session starts claude bare", ResumeNone, session.Session{}, nil},
		{
			"new session ignores a stale id",
			ResumeNone,
			session.Session{ClaudeSessionID: "derived-id"},
			nil,
		},
		{
			"reopen resumes when the derived transcript exists",
			ResumeStored,
			session.Session{ClaudeSessionID: "derived-id"},
			[]string{"--resume", "derived-id"},
		},
		{
			"reopen resumes when the stored transcript path exists",
			ResumeStored,
			session.Session{ClaudeSessionID: "stored-id", ClaudeTranscriptPath: stored},
			[]string{"--resume", "stored-id"},
		},
		{
			"reopen continues when the transcript is gone",
			ResumeStored,
			session.Session{ClaudeSessionID: "purged-id"},
			[]string{"--continue"},
		},
		{
			"reopen continues when the stored transcript path is gone",
			ResumeStored,
			session.Session{
				ClaudeSessionID:      "derived-id", // its derived file exists, but the record knows better
				ClaudeTranscriptPath: filepath.Join(home, "gone.jsonl"),
			},
			[]string{"--continue"},
		},
		{"reopen without id continues", ResumeStored, session.Session{}, []string{"--continue"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.sess.Name, tc.sess.Dir, tc.sess.ID = "my-session", "/work/demo", "sid-1"
			args := claudeArgs(&tc.sess, tc.mode)

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
				"KS_SESSION_ID=sid-1",
			}
			for _, env := range wantEnv {
				if !hasEnv(args, env) {
					t.Errorf("args %q: missing --env %s", args, env)
				}
			}
		})
	}
}

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
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
			name:      "split layout, no config",
			cfg:       nil,
			wantCalls: splitLaunch,
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
			if got.ID == "" || !hasEnv(b.launchArgs, "KS_SESSION_ID="+got.ID) {
				t.Errorf("ID %q not exported as KS_SESSION_ID in %q", got.ID, b.launchArgs)
			}
		})
	}
}

// TestOpenSavesBeforeLaunch stands in for the SessionStart hook: it fires
// inside LaunchTab, must find the record already saved and active, and its
// update must survive the launcher's own save of the kitty IDs.
func TestOpenSavesBeforeLaunch(t *testing.T) {
	store := newTestStore(t)
	var atLaunch *session.Session
	b := &fakeBackend{}
	b.onLaunch = func() {
		sess, err := store.Load("demo")
		if err != nil {
			t.Errorf("record missing when claude starts: %v", err)
			return
		}
		atLaunch = sess
		sess.ClaudeSessionID = "from-hook"
		sess.ClaudeTranscriptPath = "/t/from-hook.jsonl"
		if err := store.Save(sess); err != nil {
			t.Error(err)
		}
	}

	res, err := open(store, nil, b, Request{Name: "demo", Dir: "/work/demo"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if atLaunch == nil {
		t.Fatal("LaunchTab never ran")
	}
	if atLaunch.Status != session.StatusActive || atLaunch.KittyTabID != 0 {
		t.Errorf("record at launch = status %q tab %d, want active with no tab yet",
			atLaunch.Status, atLaunch.KittyTabID)
	}
	got, err := store.Load("demo")
	if err != nil {
		t.Fatal(err)
	}
	if got.ClaudeSessionID != "from-hook" || got.ClaudeTranscriptPath != "/t/from-hook.jsonl" {
		t.Errorf(
			"hook update lost: id %q transcript %q",
			got.ClaudeSessionID,
			got.ClaudeTranscriptPath,
		)
	}
	checkIDs(t, got, fakeTab, fakeClaudeWindow, 0, 0)
	if res.Session.ClaudeSessionID != "from-hook" {
		t.Errorf(
			"Result.Session.ClaudeSessionID = %q, want the reloaded record",
			res.Session.ClaudeSessionID,
		)
	}
}

func TestOpenStoredSession(t *testing.T) {
	live := session.Session{
		KittyTabID: 3, KittyWindowID: 9, KittyShellWindowID: 5, KittySummaryWindowID: 6,
	}
	tests := []struct {
		name        string
		stored      session.Session
		tabAlive    bool
		windowAlive bool
		closeErr    error
		wantFocused bool
		wantCalls   []string // nil skips the check
		wantResume  []string // trailing claude args when launched
		wantWarns   int
	}{
		{
			name:        "live claude window is focused, nothing launched",
			stored:      live,
			tabAlive:    true,
			windowAlive: true,
			wantFocused: true,
		},
		{
			name:        "stopped record with a live claude window still just focuses",
			stored:      withStatus(live, session.StatusStopped),
			tabAlive:    true,
			windowAlive: true,
			wantFocused: true,
		},
		{
			name:        "legacy record without window id trusts the tab",
			stored:      session.Session{KittyTabID: 3},
			tabAlive:    true,
			wantFocused: true,
		},
		{
			name:     "live tab with dead claude window is closed and relaunched",
			stored:   live,
			tabAlive: true,
			// claudeAlive, then closeTabs re-checks the tab and the two side windows.
			wantCalls: append(
				[]string{
					"TabExists", "WindowExists",
					"TabExists", "CloseTab", "WindowExists", "WindowExists",
				},
				splitLaunch...,
			),
			wantResume: []string{"--continue"},
		},
		{
			name:       "tab that will not close is a warning, not a failure",
			stored:     live,
			tabAlive:   true,
			closeErr:   errors.New("kitty says no"),
			wantResume: []string{"--continue"},
			wantWarns:  1,
		},
		{
			name: "dead tab with claude id and transcript resumes by id",
			stored: session.Session{
				KittyTabID: 3, Status: session.StatusStopped, ClaudeSessionID: "uuid-1",
				ClaudeTranscriptPath: "TRANSCRIPT",
			},
			wantCalls:  append([]string{"TabExists", "TabExists"}, splitLaunch...),
			wantResume: []string{"--resume", "uuid-1"},
		},
		{
			name:       "dead tab with claude id but no transcript continues",
			stored:     session.Session{KittyTabID: 3, ClaudeSessionID: "uuid-gone"},
			wantResume: []string{"--continue"},
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
			if tc.stored.ClaudeTranscriptPath == "TRANSCRIPT" {
				tc.stored.ClaudeTranscriptPath = filepath.Join(t.TempDir(), "uuid-1.jsonl")
				touch(t, tc.stored.ClaudeTranscriptPath)
			}
			if err := store.Save(&tc.stored); err != nil {
				t.Fatal(err)
			}
			b := &fakeBackend{
				tabAlive:    tc.tabAlive,
				windowAlive: tc.windowAlive,
				closeErr:    tc.closeErr,
			}

			res, err := open(store, nil, b, Request{Name: "demo", Resume: ResumeStored})
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			if res.Focused != tc.wantFocused {
				t.Fatalf("Focused = %v, want %v (calls %v)", res.Focused, tc.wantFocused, b.calls)
			}
			if tc.wantCalls != nil && !slices.Equal(b.calls, tc.wantCalls) {
				t.Errorf("calls = %v, want %v", b.calls, tc.wantCalls)
			}
			if len(res.Warnings) != tc.wantWarns {
				t.Errorf("warnings = %v, want %d", res.Warnings, tc.wantWarns)
			}
			got, err := store.Load("demo")
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantFocused {
				if slices.Contains(b.calls, "LaunchTab") {
					t.Errorf("focused session relaunched: %v", b.calls)
				}
				if got.Status != tc.stored.Status {
					t.Errorf(
						"Status = %q, want %q untouched by a focus",
						got.Status,
						tc.stored.Status,
					)
				}
				return
			}

			i := slices.Index(b.launchArgs, "claude")
			if tail := b.launchArgs[i+1:]; !slices.Equal(tail, tc.wantResume) {
				t.Errorf("claude args = %q, want %q", tail, tc.wantResume)
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
			if got.ID == "" || !hasEnv(b.launchArgs, "KS_SESSION_ID="+got.ID) {
				t.Errorf("reopened record got ID %q, exported in %q", got.ID, b.launchArgs)
			}
		})
	}
}

func withStatus(s session.Session, status string) session.Session {
	s.Status = status
	return s
}

func TestOpenNewSessionNameTaken(t *testing.T) {
	store := newTestStore(t)
	if err := store.Save(session.New("taken", "/work/old", 1, 2)); err != nil {
		t.Fatal(err)
	}
	b := &fakeBackend{}
	_, err := open(store, nil, b, Request{Name: "taken", Dir: "/work/new"})
	if !errors.Is(err, ErrExists) {
		t.Fatalf("err = %v, want ErrExists", err)
	}
	if !strings.Contains(err.Error(), `session "taken" already exists`) {
		t.Errorf("err = %q, want it to name the session", err)
	}
	if len(b.calls) != 0 {
		t.Errorf("kitty was driven for a rejected name: %v", b.calls)
	}
	got, err := store.Load("taken")
	if err != nil || got.Dir != "/work/old" {
		t.Errorf("existing record touched: %+v, %v", got, err)
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
