package launcher

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mad01/kitty-session/internal/claude"
	"github.com/mad01/kitty-session/internal/kitty"
	"github.com/mad01/kitty-session/internal/session"
)

func TestClaudeCmd(t *testing.T) {
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
		{
			"reopen in a dir with no transcript starts fresh",
			ResumeStored,
			session.Session{Dir: "/work/untouched"},
			nil,
		},
		{
			"reopen with a purged id in a dir with no transcript starts fresh",
			ResumeStored,
			session.Session{Dir: "/work/untouched", ClaudeSessionID: "purged-id"},
			nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.sess.Dir == "" {
				tc.sess.Dir = "/work/demo" // its project dir holds derived-id.jsonl
			}
			cmd := claudeCmd(&tc.sess, tc.mode)
			if cmd[0] != "claude" {
				t.Fatalf("cmd %q: want it to start claude", cmd)
			}
			if tail := cmd[1:]; !slices.Equal(tail, tc.wantTail) {
				t.Errorf("claude args = %q, want %q", tail, tc.wantTail)
			}
		})
	}
}

func TestSessionEnv(t *testing.T) {
	t.Setenv("PATH", "/opt/claude/bin:/usr/bin")
	sess := &session.Session{Name: "my-session", ID: "sid-1"}
	want := []string{
		"PATH=/opt/claude/bin:/usr/bin",
		"KS_SESSION_NAME=my-session",
		"KS_SESSION_ID=sid-1",
	}
	if got := sessionEnv(sess); !slices.Equal(got, want) {
		t.Errorf("sessionEnv = %q, want %q", got, want)
	}
}

func TestClaudeBias(t *testing.T) {
	l := &Launcher{sidebarWidth: 36}
	tests := []struct{ total, want int }{
		{158, 77},
		{200, 82},
		{0, defaultClaudeBias},
		{36, minBias},    // no room: claude keeps the minimum share
		{10000, maxBias}, // huge tab: clamp so the sidebar stays visible
	}
	for _, tc := range tests {
		if got := l.claudeBias(tc.total); got != tc.want {
			t.Errorf("claudeBias(%d) = %d, want %d", tc.total, got, tc.want)
		}
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

func TestOpenNewSession(t *testing.T) {
	t.Setenv("PATH", "/opt/claude/bin:/usr/bin")
	l, f, store := newTestLauncher(t)

	res, err := l.Open(Request{Name: "demo", Dir: "/work/demo"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	want := append(newTabLaunch(2, 3, "demo"), "Windows", "CloseTab(1)") // the home tab retires
	if !slices.Equal(f.calls, want) {
		t.Errorf("calls =\n%v\nwant\n%v", f.calls, want)
	}
	if len(res.Warnings) != 0 || res.Focused {
		t.Errorf("Result = focused %v warnings %v, want a clean create", res.Focused, res.Warnings)
	}

	got, err := store.Load("demo")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	checkIDs(t, got, 2, 2, 3)
	if got.Status != session.StatusActive {
		t.Errorf("Status = %q, want active", got.Status)
	}
	if got.Dir != "/work/demo" {
		t.Errorf("Dir = %q, want /work/demo", got.Dir)
	}
	if got.FocusedAt.IsZero() {
		t.Error("FocusedAt not stamped on create")
	}
	if got.ID == "" {
		t.Fatal("record has no ID")
	}
	if w := f.find(2); w == nil || w.Columns != 36 {
		t.Errorf("sidebar = %+v, want 36 columns after the pin", w)
	}

	if len(f.launches) != 2 {
		t.Fatalf("launches = %d, want sidebar and claude", len(f.launches))
	}
	sidebar, claudeWin := f.launches[0], f.launches[1]
	if want := []string{"/bin/ks", "sidebar", "--session-id", got.ID}; !slices.Equal(
		sidebar.Command,
		want,
	) {
		t.Errorf("sidebar command = %q, want %q", sidebar.Command, want)
	}
	if want := []string{"claude"}; !slices.Equal(claudeWin.Command, want) {
		t.Errorf("claude command = %q, want %q", claudeWin.Command, want)
	}
	for _, launch := range f.launches {
		if launch.Dir != "/work/demo" {
			t.Errorf("launch dir = %q, want /work/demo", launch.Dir)
		}
		if want := []string{kitty.SessionVar + "=" + got.ID}; !slices.Equal(launch.Vars, want) {
			t.Errorf("launch vars = %q, want %q", launch.Vars, want)
		}
		for _, env := range []string{
			"PATH=/opt/claude/bin:/usr/bin", "KS_SESSION_NAME=demo", "KS_SESSION_ID=" + got.ID,
		} {
			if !hasEnv(launch, env) {
				t.Errorf("launch %q lacks env %s", launch.Command, env)
			}
		}
	}
}

func TestOpenGeometryAndFocusFailuresAreWarnings(t *testing.T) {
	l, f, store := newTestLauncher(t)
	f.errs["LayoutAction"] = errors.New("no such action")
	f.errs["ResizeWindow"] = errors.New("cannot resize")
	f.errs["FocusWindow(3)"] = errors.New("no focus") // claude only; the sidebar focus works

	res, err := l.Open(Request{Name: "demo", Dir: "/work/demo"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(res.Warnings) != 3 {
		t.Errorf("warnings = %v, want 3", res.Warnings)
	}
	got, err := store.Load("demo")
	if err != nil {
		t.Fatal(err)
	}
	checkIDs(t, got, 2, 2, 3)
}

// TestOpenSavesBeforeLaunch stands in for the SessionStart hook: it fires
// inside LaunchTab, must find the record already saved and active, and its
// update must survive the launcher's own save of the kitty IDs.
func TestOpenSavesBeforeLaunch(t *testing.T) {
	l, f, store := newTestLauncher(t)
	var atLaunch *session.Session
	f.onLaunch = func() {
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

	res, err := l.Open(Request{Name: "demo", Dir: "/work/demo"})
	if err != nil {
		t.Fatalf("Open: %v", err)
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
		t.Errorf("hook update lost: id %q transcript %q",
			got.ClaudeSessionID, got.ClaudeTranscriptPath)
	}
	checkIDs(t, got, 2, 2, 3)
	if res.Session.ClaudeSessionID != "from-hook" {
		t.Errorf("Result.Session.ClaudeSessionID = %q, want the reloaded record",
			res.Session.ClaudeSessionID)
	}
}

// withHomeRetired appends the home tab's retirement, which ends every Open
// that launched windows while the home tab was still there.
func withHomeRetired(calls []string) []string {
	return append(calls, "Windows", "CloseTab(1)")
}

// relaunchCalls is the sequence that puts claude back beside a surviving
// sidebar spanning the tab.
func relaunchCalls(sidebar, claude int) []string {
	return []string{
		"Windows", // liveness
		"GotoLayout(2,splits)",
		"LaunchVSplit(2,bias=77)",
		"FocusWindow(2)",
		"LayoutAction(2,move_to_screen_edge left)",
		"Windows",
		"ResizeWindow(2,horizontal,-1)",
		"Windows",
		"FocusWindow(3)",
	}
}

func TestOpenStoredSession(t *testing.T) {
	tests := []struct {
		name        string
		stored      session.Session
		arrange     func(f *fakeKitty, sess *session.Session)
		closeErr    error
		transcripts bool // the dir has a transcript, so a relaunch can --continue
		wantFocused bool
		wantCalls   []string // nil skips the check
		wantResume  []string // trailing claude args when launched
		wantWarns   int
		wantIDs     [3]int // tab, sidebar, claude after the call
	}{
		{
			name:        "live claude window is focused, nothing launched",
			arrange:     func(f *fakeKitty, s *session.Session) { f.addTab(s, true) },
			wantFocused: true,
			wantCalls:   []string{"Windows", "CloseTab(1)", "FocusWindow(3)"}, // home retires first
			wantIDs:     [3]int{2, 2, 3},
		},
		{
			name:        "stopped record with a live claude window still just focuses",
			stored:      session.Session{Status: session.StatusStopped},
			arrange:     func(f *fakeKitty, s *session.Session) { f.addTab(s, true) },
			wantFocused: true,
			wantIDs:     [3]int{2, 2, 3},
		},
		{
			name: "window ids reused by another session are not ours",
			arrange: func(f *fakeKitty, s *session.Session) {
				other := session.New("other", "/work/other", 0, 0)
				f.addTab(other, true) // windows 2 and 3, tagged with other's id
				s.KittyTabID, s.KittySidebarWindowID, s.KittyWindowID = 2, 2, 3
			},
			// No CloseTab(2): the tab is not ours. A new tab gets windows 4 and 5.
			wantCalls: withHomeRetired(append([]string{"Windows"}, newTabLaunch(4, 5, "demo")...)),
			wantIDs:   [3]int{3, 4, 5},
		},
		{
			name: "surviving sidebar gets claude relaunched beside it",
			arrange: func(f *fakeKitty, s *session.Session) {
				f.addTab(s, false)
				s.KittyWindowID = 9 // the claude window that exited
			},
			transcripts: true,
			wantCalls:   withHomeRetired(relaunchCalls(2, 3)),
			wantResume:  []string{"--continue"},
			wantIDs:     [3]int{2, 2, 3},
		},
		{
			name: "stray owned windows are closed before the relaunch",
			arrange: func(f *fakeKitty, s *session.Session) {
				f.addTab(s, true)
				s.KittySidebarWindowID, s.KittyWindowID = 8, 9 // record out of sync
			},
			wantCalls: withHomeRetired(
				append([]string{"Windows", "CloseTab(2)"}, newTabLaunch(4, 5, "demo")...),
			),
			wantIDs: [3]int{3, 4, 5},
		},
		{
			name: "tab that will not close is a warning, not a failure",
			arrange: func(f *fakeKitty, s *session.Session) {
				f.addTab(s, true)
				s.KittySidebarWindowID, s.KittyWindowID = 8, 9
			},
			closeErr:  errors.New("kitty says no"),
			wantWarns: 1,
			wantIDs:   [3]int{3, 4, 5},
		},
		{
			name: "dead tab with claude id and transcript resumes by id",
			stored: session.Session{
				KittyTabID: 3, Status: session.StatusStopped, ClaudeSessionID: "uuid-1",
				ClaudeTranscriptPath: "TRANSCRIPT",
			},
			wantCalls:  withHomeRetired(append([]string{"Windows"}, newTabLaunch(2, 3, "demo")...)),
			wantResume: []string{"--resume", "uuid-1"},
			wantIDs:    [3]int{2, 2, 3},
		},
		{
			name:        "dead tab with claude id but no own transcript continues",
			stored:      session.Session{KittyTabID: 3, ClaudeSessionID: "uuid-gone"},
			transcripts: true,
			wantResume:  []string{"--continue"},
			wantIDs:     [3]int{2, 2, 3},
		},
		{
			name:    "dead tab in a dir without transcripts starts fresh",
			stored:  session.Session{KittyTabID: 3, ClaudeSessionID: "uuid-gone"},
			wantIDs: [3]int{2, 2, 3},
		},
		{
			name:    "legacy record without id gets one and starts fresh",
			stored:  session.Session{ID: "", KittyTabID: 3, KittyWindowID: 4},
			wantIDs: [3]int{2, 2, 3},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l, f, store := newTestLauncher(t)
			sess := tc.stored
			sess.Name, sess.Dir = "demo", "/work/demo"
			if tc.name != "legacy record without id gets one and starts fresh" && sess.ID == "" {
				sess.ID = session.NewID()
			}
			if tc.transcripts {
				other, err := claude.TranscriptPath(sess.Dir, "someone-else")
				if err != nil {
					t.Fatal(err)
				}
				touch(t, other)
			}
			if sess.ClaudeTranscriptPath == "TRANSCRIPT" {
				sess.ClaudeTranscriptPath = filepath.Join(t.TempDir(), "uuid-1.jsonl")
				touch(t, sess.ClaudeTranscriptPath)
			}
			if tc.arrange != nil {
				tc.arrange(f, &sess)
			}
			if err := store.Save(&sess); err != nil {
				t.Fatal(err)
			}
			f.errs["CloseTab(2)"] = tc.closeErr // the stray tab; the home tab still closes
			f.calls, f.launches = nil, nil

			res, err := l.Open(Request{Name: "demo", Resume: ResumeStored})
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			if res.Focused != tc.wantFocused {
				t.Fatalf("Focused = %v, want %v (calls %v)", res.Focused, tc.wantFocused, f.calls)
			}
			if tc.wantCalls != nil && !slices.Equal(f.calls, tc.wantCalls) {
				t.Errorf("calls =\n%v\nwant\n%v", f.calls, tc.wantCalls)
			}
			if len(res.Warnings) != tc.wantWarns {
				t.Errorf("warnings = %v, want %d", res.Warnings, tc.wantWarns)
			}
			got, err := store.Load("demo")
			if err != nil {
				t.Fatal(err)
			}
			checkIDs(t, got, tc.wantIDs[0], tc.wantIDs[1], tc.wantIDs[2])
			if got.FocusedAt.IsZero() {
				t.Error("FocusedAt not stamped")
			}
			if tc.wantFocused {
				if len(f.launches) != 0 {
					t.Errorf("focused session launched windows: %v", f.calls)
				}
				if got.Status != sess.Status {
					t.Errorf("Status = %q, want %q untouched by a focus", got.Status, sess.Status)
				}
				return
			}

			claudeLaunch := f.launches[len(f.launches)-1]
			if tail := claudeLaunch.Command[1:]; !slices.Equal(tail, tc.wantResume) {
				t.Errorf("claude args = %q, want %q", tail, tc.wantResume)
			}
			if got.Status != session.StatusActive {
				t.Errorf("Status = %q, want active after reopen", got.Status)
			}
			if got.ClaudeSessionID != sess.ClaudeSessionID {
				t.Errorf(
					"ClaudeSessionID = %q, want %q kept",
					got.ClaudeSessionID,
					sess.ClaudeSessionID,
				)
			}
			if got.ID == "" {
				t.Fatal("reopened record has no ID")
			}
			for _, launch := range f.launches {
				if !hasEnv(launch, "KS_SESSION_ID="+got.ID) || varValue(launch.Vars) != got.ID {
					t.Errorf("launch %q not tagged with id %q: env %q vars %q",
						launch.Command, got.ID, launch.Env, launch.Vars)
				}
			}
		})
	}
}

func TestOpenNewSessionNameTaken(t *testing.T) {
	l, f, store := newTestLauncher(t)
	if err := store.Save(session.New("taken", "/work/old", 1, 2)); err != nil {
		t.Fatal(err)
	}
	_, err := l.Open(Request{Name: "taken", Dir: "/work/new"})
	if !errors.Is(err, ErrExists) {
		t.Fatalf("err = %v, want ErrExists", err)
	}
	if !strings.Contains(err.Error(), `session "taken" already exists`) {
		t.Errorf("err = %q, want it to name the session", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("kitty was driven for a rejected name: %v", f.calls)
	}
	got, err := store.Load("taken")
	if err != nil || got.Dir != "/work/old" {
		t.Errorf("existing record touched: %+v, %v", got, err)
	}
}

func TestOpenStoredSessionMissing(t *testing.T) {
	l, _, _ := newTestLauncher(t)
	_, err := l.Open(Request{Name: "ghost", Resume: ResumeStored})
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("err = %v, want not-found naming ghost", err)
	}
}

func TestOpenStoredSessionInstanceDown(t *testing.T) {
	l, f, store := newTestLauncher(t)
	if err := store.Save(session.New("demo", "/work/demo", 1, 2)); err != nil {
		t.Fatal(err)
	}
	f.errs["Windows"] = errors.New("connection refused")
	_, err := l.Open(Request{Name: "demo", Resume: ResumeStored})
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("err = %v, want the kitty failure", err)
	}
	if len(f.launches) != 0 {
		t.Error("launched into an unreachable instance")
	}
}

func TestAlive(t *testing.T) {
	l, f, _ := newTestLauncher(t)
	sess := session.New("demo", "/work/demo", 0, 0)
	if l.Alive(sess) {
		t.Error("Alive with no windows")
	}
	f.addTab(sess, false)
	if l.Alive(sess) {
		t.Error("Alive with only a sidebar")
	}
	f.addTab(sess, true)
	if !l.Alive(sess) {
		t.Error("not Alive with a tagged claude window")
	}
	f.errs["Windows"] = errors.New("down")
	if l.Alive(sess) {
		t.Error("Alive while the instance is unreachable")
	}
}
