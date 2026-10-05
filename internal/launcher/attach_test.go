package launcher

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mad01/kitty-session/internal/session"
)

// attachSpec describes one stored session before an attach.
type attachSpec struct {
	name      string
	stopped   bool
	focusedAt time.Duration // offset from t0; 0 means never focused
	alive     bool          // sidebar and claude windows in the instance
	sidebar   bool          // only the sidebar window survived
}

func TestAttach(t *testing.T) {
	t0 := time.Date(2026, 10, 5, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name         string
		sessions     []attachSpec
		launchErr    error
		claudeExits  bool
		wantResumed  int
		wantRunning  int
		wantStopped  int
		wantExited   []string
		wantFocused  string
		wantWarns    int
		wantLaunches int // LaunchTab calls
		wantSleeps   int // attachStagger pauses
		wantSettle   bool
		wantHome     bool
	}{
		{
			name:     "empty store focuses the home tab",
			wantHome: true,
		},
		{
			name: "all running: nothing launched, latest focus wins",
			sessions: []attachSpec{
				{name: "alpha", focusedAt: time.Hour, alive: true},
				{name: "bravo", focusedAt: 2 * time.Hour, alive: true},
			},
			wantRunning: 2,
			wantFocused: "bravo",
		},
		{
			name: "dead sessions resume with a stagger, stopped ones stay down",
			sessions: []attachSpec{
				{name: "alpha", focusedAt: time.Hour, alive: true},
				{name: "bravo", focusedAt: 3 * time.Hour},
				{name: "charlie"},
				{name: "delta", stopped: true},
			},
			wantResumed:  2,
			wantRunning:  1,
			wantStopped:  1,
			wantFocused:  "bravo",
			wantLaunches: 2,
			wantSleeps:   1,
			wantSettle:   true,
		},
		{
			name: "never focused: the oldest active is focused",
			sessions: []attachSpec{
				{name: "bravo"},
				{name: "alpha"},
				{name: "aardvark", stopped: true},
			},
			wantResumed:  2,
			wantStopped:  1,
			wantFocused:  "bravo",
			wantLaunches: 2,
			wantSleeps:   1,
			wantSettle:   true,
		},
		{
			name:         "a session that will not launch is a warning, home gets focus",
			sessions:     []attachSpec{{name: "alpha", focusedAt: time.Hour}},
			launchErr:    errors.New("kitty says no"),
			wantWarns:    1,
			wantLaunches: 1,
			wantHome:     true,
		},
		{
			name:        "a surviving sidebar gets claude back in place",
			sessions:    []attachSpec{{name: "alpha", sidebar: true}},
			wantResumed: 1,
			wantFocused: "alpha",
			wantSettle:  true,
		},
		{
			name: "a claude that exits right after launch is reported, not counted",
			sessions: []attachSpec{
				{name: "alpha", focusedAt: time.Hour},
				{name: "bravo", alive: true},
			},
			claudeExits:  true,
			wantRunning:  1,
			wantExited:   []string{"alpha"},
			wantLaunches: 1,
			wantSettle:   true,
			wantHome:     true, // alpha was the target; it is not focused
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l, f, store := newTestLauncher(t)
			for i, spec := range tc.sessions {
				sess := session.New(spec.name, "/work/"+spec.name, 70, 71)
				// Listed order is creation order, so the fallback focus is deterministic.
				sess.CreatedAt = t0.Add(time.Duration(i) * time.Minute).Format(time.RFC3339Nano)
				sess.KittySidebarWindowID = 72 // stale ids: nothing in the instance
				if spec.stopped {
					sess.Status = session.StatusStopped
				}
				if spec.focusedAt != 0 {
					sess.FocusedAt = t0.Add(spec.focusedAt)
				}
				if spec.alive || spec.sidebar {
					f.addTab(sess, spec.alive)
				}
				if err := store.Save(sess); err != nil {
					t.Fatal(err)
				}
			}
			f.errs["LaunchTab"] = tc.launchErr
			f.claudeExits = tc.claudeExits
			f.calls = nil

			res, err := l.Attach()
			if err != nil {
				t.Fatalf("Attach: %v", err)
			}
			got := [3]int{res.Resumed, res.Running, res.Stopped}
			want := [3]int{tc.wantResumed, tc.wantRunning, tc.wantStopped}
			if got != want {
				t.Errorf("resumed/running/stopped = %v, want %v", got, want)
			}
			if res.Focused != tc.wantFocused {
				t.Errorf("Focused = %q, want %q", res.Focused, tc.wantFocused)
			}
			if !slices.Equal(res.Exited, tc.wantExited) {
				t.Errorf("Exited = %v, want %v", res.Exited, tc.wantExited)
			}
			if len(res.Warnings) != tc.wantWarns {
				t.Errorf("warnings = %v, want %d", res.Warnings, tc.wantWarns)
			}
			launches := 0
			for _, c := range f.calls {
				if c == "LaunchTab" {
					launches++
				}
			}
			if launches != tc.wantLaunches {
				t.Errorf(
					"LaunchTab calls = %d, want %d (calls %v)",
					launches,
					tc.wantLaunches,
					f.calls,
				)
			}
			wantSlept := time.Duration(tc.wantSleeps) * attachStagger
			if tc.wantSettle {
				wantSlept += settleAfterLaunch
			}
			if f.slept != wantSlept {
				t.Errorf("slept %v, want %v (%d stagger, settle %v)",
					f.slept, wantSlept, tc.wantSleeps, tc.wantSettle)
			}
			checkAttachFocus(t, f, store, tc.wantFocused, tc.wantHome)
		})
	}
}

// checkAttachFocus verifies the last focus landed on the named session's
// claude window (or the home window), and that the focused session carries
// the newest FocusedAt so the next attach picks it again.
func checkAttachFocus(t *testing.T, f *fakeKitty, store *session.Store, focused string, home bool) {
	t.Helper()
	last := f.calls[len(f.calls)-1]
	if home {
		// The home tab when it is still there, else the instance's first window.
		if want := fmt.Sprintf("FocusWindow(%d)", f.windows[0].ID); last != want {
			t.Errorf("last call = %s, want %s", last, want)
		}
		return
	}
	target, err := store.Load(focused)
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("FocusWindow(%d)", target.KittyWindowID); last != want {
		t.Errorf("last call = %s, want %s", last, want)
	}
	if !strings.HasPrefix(last, "FocusWindow(") {
		t.Errorf("attach did not end with a focus: %v", f.calls)
	}
	all, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	newest := slices.MaxFunc(all, func(a, b *session.Session) int {
		return a.FocusedAt.Compare(b.FocusedAt)
	})
	if newest.Name != focused {
		t.Errorf("newest FocusedAt is %s, want %s", newest.Name, focused)
	}
}

// TestResumeLaunchesOldestFirstWithoutFocus covers the cold-start path of
// new, open and tmp: every active session comes back in creation order,
// hidden, and nothing is focused.
func TestResumeLaunchesOldestFirstWithoutFocus(t *testing.T) {
	l, f, store := newTestLauncher(t)
	t0 := time.Date(2026, 10, 5, 8, 0, 0, 0, time.UTC)
	for i, name := range []string{"zulu", "alpha"} { // zulu is the older one
		sess := session.New(name, "/work/"+name, 70, 71)
		sess.CreatedAt = t0.Add(time.Duration(i) * time.Minute).Format(time.RFC3339Nano)
		if err := store.Save(sess); err != nil {
			t.Fatal(err)
		}
	}
	res, err := l.Resume()
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if res.Resumed != 2 || res.Focused != "" {
		t.Errorf("result = %+v, want 2 resumed and nothing focused", res)
	}
	var titles []string
	for _, c := range f.calls {
		if strings.HasPrefix(c, "FocusWindow(") {
			t.Errorf("Resume focused a window: %v", f.calls)
		}
		if strings.HasPrefix(c, "SetTabTitle(") {
			titles = append(titles, c)
		}
	}
	if len(titles) != 2 || !strings.HasSuffix(titles[0], ",zulu)") ||
		!strings.HasSuffix(titles[1], ",alpha)") {
		t.Errorf("tab order = %v, want zulu then alpha", titles)
	}
}
