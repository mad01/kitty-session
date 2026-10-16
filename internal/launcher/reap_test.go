package launcher

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/mad01/kitty-session/internal/kitty"
	"github.com/mad01/kitty-session/internal/session"
)

// dropWindow removes one window from the fake, as kitty does when the
// process in it exits.
func dropWindow(f *fakeKitty, id int) {
	f.windows = slices.DeleteFunc(f.windows, func(w kitty.Window) bool { return w.ID == id })
}

// dropClaude removes the session's claude window.
func dropClaude(f *fakeKitty, own *session.Session) { dropWindow(f, own.KittyWindowID) }

// newOwnBackend wires a backend for own's tab, with the home tab already
// retired as Open does once a session tab exists.
func newOwnBackend(
	t *testing.T,
	own *session.Session,
	withClaude bool,
) (*SidebarBackend, *fakeKitty, *time.Time) {
	t.Helper()
	b, f, clock := newTestBackend(t)
	b.ownID = own.ID
	f.addTab(own, withClaude)
	if err := b.l.store.Save(own); err != nil {
		t.Fatal(err)
	}
	dropWindow(f, 1)
	return b, f, clock
}

// listCalls runs one List and returns the calls it made to the fake.
func listCalls(t *testing.T, b *SidebarBackend, f *fakeKitty) []string {
	t.Helper()
	f.calls = nil
	if _, err := b.List(); err != nil {
		t.Fatalf("List: %v", err)
	}
	return f.calls
}

func TestListReapsOwnTabOnceClaudeIsGone(t *testing.T) {
	shellSplit := func(f *fakeKitty, own *session.Session) {
		dropClaude(f, own)
		_, err := f.LaunchHSplit(kitty.Launch{
			Match: own.KittySidebarWindowID,
			Vars:  []string{kitty.SessionVar + "=" + own.ID},
		})
		if err != nil {
			panic(err)
		}
	}
	tests := []struct {
		name       string
		withClaude bool                                     // claude is in the first snapshot
		otherTab   bool                                     // another session's tab exists
		between    func(f *fakeKitty, own *session.Session) // runs before the second List
		advance    time.Duration                            // clock movement before the second List
		lsErr      error                                    // Windows() fails on the second List
		wantClose  bool
		wantHome   bool // the home tab is recreated before the close
	}{
		{name: "claude present", withClaude: true},
		{
			name:       "present then gone, last tab",
			withClaude: true,
			between:    dropClaude,
			wantClose:  true,
			wantHome:   true,
		},
		{
			name:       "present then gone, another session tab stays",
			withClaude: true,
			otherTab:   true,
			between:    dropClaude,
			wantClose:  true,
		},
		{name: "never present within the grace", advance: reapGrace - time.Second},
		{name: "never present past the grace", advance: reapGrace, wantClose: true, wantHome: true},
		{
			name:       "gone but a window the record does not name lives on",
			withClaude: true,
			between:    shellSplit,
			advance:    reapGrace,
		},
		{
			name:       "instance unreachable",
			withClaude: true,
			between:    dropClaude,
			lsErr:      errors.New("connection refused"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			own := session.New("own", "/work/own", 0, 0)
			b, f, clock := newOwnBackend(t, own, tt.withClaude)
			if tt.otherTab {
				other := session.New("other", "/work/other", 0, 0)
				f.addTab(other, true)
				if err := b.l.store.Save(other); err != nil {
					t.Fatal(err)
				}
			}
			if calls := listCalls(t, b, f); hasPrefixCall(f, "CloseTab(") {
				t.Fatalf("first List closed a tab: %v", calls)
			}
			if tt.between != nil {
				tt.between(f, own)
			}
			*clock = clock.Add(tt.advance)
			f.calls = nil
			f.errs["Windows"] = tt.lsErr
			_, err := b.List()
			if (err != nil) != (tt.lsErr != nil) {
				t.Fatalf("List err = %v, want failure %v", err, tt.lsErr != nil)
			}
			if n := countCalls(f, "Windows"); n != 1 {
				t.Errorf("List took %d snapshots, want 1: %v", n, f.calls)
			}
			closed := slices.Contains(f.calls, "CloseTab("+itoa(own.KittyTabID)+")")
			if closed != tt.wantClose {
				t.Errorf("own tab closed = %v, want %v: calls %v", closed, tt.wantClose, f.calls)
			}
			if home := slices.Contains(f.calls, "LaunchTab"); home != tt.wantHome {
				t.Errorf("home tab opened = %v, want %v: calls %v", home, tt.wantHome, f.calls)
			}
			if !tt.wantHome {
				return
			}
			if len(f.windows) != 1 || f.windows[0].SessionID != "" ||
				f.windows[0].TabTitle != homeTitle {
				t.Errorf("instance should hold only the home tab, got %+v", f.windows)
			}
		})
	}
}

// TestReapEndsTheSession checks what the reap does to the record and the
// state file. A claude or pi agent whose window a snapshot first showed
// reapRan or more before the tick that finds it gone takes both away, the
// record to the trash, stopped, as the sidebar's delete sends it; so does
// one the hook marked stopped, however short its run. A shell keeps its record. So does an agent gone
// inside reapRan, or never seen within the grace, whatever the kind, so a
// launch that fails on attach is not thrown away; and one whose record no
// longer names the window the snapshots showed, a relaunch under way. A
// record the store cannot move stays in place, stopped.
func TestReapEndsTheSession(t *testing.T) {
	const never = -1
	tests := []struct {
		name       string
		kind       string
		stopped    bool          // the hook marked the record stopped before the reap
		seen       time.Duration // how long before the gone tick the window first showed; never for none
		relaunched bool          // the record carries a cleared window id, as Open leaves it mid-launch
		noTrash    bool          // the trash directory cannot take the record
		wantTrash  bool
		wantActive bool // the record stays and is active
	}{
		{name: "claude ran and is gone", kind: session.AgentClaude, seen: reapRan, wantTrash: true},
		{name: "pi ran and is gone", kind: session.AgentPi, seen: reapRan, wantTrash: true},
		{name: "shell ran and is gone", kind: session.AgentShell, seen: reapRan, wantActive: true},
		{
			name: "claude the hook marked stopped, gone at once", kind: session.AgentClaude,
			stopped: true, wantTrash: true,
		},
		{
			name: "claude gone inside reapRan", kind: session.AgentClaude,
			seen: reapRan - time.Second, wantActive: true,
		},
		{
			name: "claude relaunched meanwhile", kind: session.AgentClaude,
			seen: reapRan, relaunched: true, wantActive: true,
		},
		{name: "claude never came up", kind: session.AgentClaude, seen: never, wantActive: true},
		{name: "pi never came up", kind: session.AgentPi, seen: never, wantActive: true},
		{name: "shell never came up", kind: session.AgentShell, seen: never, wantActive: true},
		{
			name:    "claude ran, trash unwritable",
			kind:    session.AgentClaude,
			seen:    reapRan,
			noTrash: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			own := session.New("own", "/work/own", 0, 0)
			own.Agent = tt.kind
			if tt.stopped {
				own.Status = session.StatusStopped
			}
			b, f, clock := newOwnBackend(t, own, tt.seen != never)
			var cleaned []string
			b.l.cleanState = func(name string) { cleaned = append(cleaned, name) }
			if tt.noTrash {
				blockTrash(t)
			}
			listCalls(t, b, f)
			if tt.seen == never {
				*clock = clock.Add(reapGrace)
			} else {
				*clock = clock.Add(tt.seen)
				listCalls(t, b, f)
				dropClaude(f, own)
				if tt.relaunched {
					own.KittyWindowID = 0
					if err := b.l.store.Save(own); err != nil {
						t.Fatal(err)
					}
				}
			}
			if calls := listCalls(t, b, f); !hasPrefixCall(f, "CloseTab(") {
				t.Fatalf("tab not closed: %v", calls)
			}

			got, err := b.l.store.Load("own")
			trashed, _ := b.l.store.ListTrashed()
			if tt.wantTrash {
				if err == nil {
					t.Errorf("record still in the store: %+v", got)
				}
				if len(trashed) != 1 || trashed[0].ID != own.ID || trashed[0].IsActive() {
					t.Errorf("trash = %+v, want the own record, stopped", trashed)
				}
				if want := []string{"own"}; !slices.Equal(cleaned, want) {
					t.Errorf("state files cleaned = %v, want %v", cleaned, want)
				}
				return
			}
			if err != nil {
				t.Fatalf("record gone from the store: %v", err)
			}
			if got.IsActive() != tt.wantActive || got.ID != own.ID ||
				got.KittyTabID != own.KittyTabID {
				t.Errorf("record = %+v, want active %v", got, tt.wantActive)
			}
			if len(trashed) != 0 {
				t.Errorf("trash = %+v, want empty", trashed)
			}
			if len(cleaned) != 0 {
				t.Errorf("state files cleaned = %v, want none", cleaned)
			}
		})
	}
}

// blockTrash puts a file where the store's trash directory is, so a move
// into it fails.
func blockTrash(t *testing.T) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".config", "ks", "sessions", "trash")
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReapIsTriedOnce(t *testing.T) {
	own := session.New("own", "/work/own", 0, 0)
	b, f, _ := newOwnBackend(t, own, true)
	listCalls(t, b, f)
	dropClaude(f, own)
	f.errs["CloseTab"] = errors.New("kitty says no")
	if calls := listCalls(t, b, f); !hasPrefixCall(f, "CloseTab(") {
		t.Fatalf("close not attempted: %v", calls)
	}
	if f.find(own.KittySidebarWindowID) == nil {
		t.Fatal("fake closed the tab despite the error")
	}
	if calls := listCalls(t, b, f); hasPrefixCall(f, "CloseTab(") || hasPrefixCall(f, "LaunchTab") {
		t.Errorf("second List retried the close: %v", calls)
	}
}

func TestReapWaitsForTheShellSplit(t *testing.T) {
	own := session.New("own", "/work/own", 0, 0)
	b, f, _ := newOwnBackend(t, own, true)
	listCalls(t, b, f)
	shell, err := f.LaunchHSplit(kitty.Launch{
		Match: own.KittyWindowID,
		Vars:  []string{kitty.SessionVar + "=" + own.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	dropClaude(f, own)
	if calls := listCalls(t, b, f); hasPrefixCall(f, "CloseTab(") {
		t.Fatalf("tab closed under a live shell split: %v", calls)
	}
	dropWindow(f, shell)
	if calls := listCalls(t, b, f); !hasPrefixCall(f, "CloseTab(") {
		t.Errorf("tab kept after the shell split exited: %v", calls)
	}
}

// TestReapKnowsItsOwnWindowFromKitty covers a tab whose record never got
// its window ids (Open failed after creating the tab): KITTY_WINDOW_ID tells
// the sidebar which tagged window is itself, so the grace rule still fires.
func TestReapKnowsItsOwnWindowFromKitty(t *testing.T) {
	for _, named := range []bool{true, false} {
		t.Run(map[bool]string{true: "window id set", false: "window id unset"}[named],
			func(t *testing.T) {
				own := session.New("own", "/work/own", 0, 0)
				b, f, clock := newOwnBackend(t, own, false)
				sidebar := own.KittySidebarWindowID
				own.KittySidebarWindowID = 0
				if err := b.l.store.Save(own); err != nil {
					t.Fatal(err)
				}
				if named {
					t.Setenv("KITTY_WINDOW_ID", itoa(sidebar))
				} else {
					t.Setenv("KITTY_WINDOW_ID", "")
				}
				listCalls(t, b, f)
				*clock = clock.Add(reapGrace)
				calls := listCalls(t, b, f)
				if closed := hasPrefixCall(f, "CloseTab("); closed != named {
					t.Errorf("tab closed = %v, want %v: calls %v", closed, named, calls)
				}
			})
	}
}

func TestHomeSidebarNeverReaps(t *testing.T) {
	b, f, clock := newTestBackend(t)
	dead := session.New("dead", "/work/dead", 0, 0)
	f.addTab(dead, false)
	if err := b.l.store.Save(dead); err != nil {
		t.Fatal(err)
	}
	listCalls(t, b, f)
	*clock = clock.Add(2 * reapGrace)
	if calls := listCalls(t, b, f); hasPrefixCall(f, "CloseTab(") || hasPrefixCall(f, "LaunchTab") {
		t.Errorf("home sidebar touched a tab: %v", calls)
	}
}

// TestReapWithdrawsTheNotificationFirst checks that the own session's
// notification is taken down before its tab is closed, while the sidebar
// window that posted it is still there to do so.
func TestReapWithdrawsTheNotificationFirst(t *testing.T) {
	own := session.New("own", "/work/own", 0, 0)
	b, f, _ := newOwnBackend(t, own, true)
	var ran []int // how many kitty calls the poll had made when withdraw ran
	b.withdraw = func() { ran = append(ran, len(f.calls)) }
	listCalls(t, b, f) // claude seen
	dropClaude(f, own)
	calls := listCalls(t, b, f)
	closeAt := slices.IndexFunc(calls, func(c string) bool {
		return len(c) > 9 && c[:9] == "CloseTab("
	})
	if closeAt < 0 {
		t.Fatalf("tab not closed: %v", calls)
	}
	if len(ran) != 1 || ran[0] > closeAt {
		t.Fatalf("withdraw ran at kitty calls %v, want once before the close at %d", ran, closeAt)
	}
	listCalls(t, b, f)
	if len(ran) != 1 {
		t.Errorf("withdraw ran again on a later poll: %v", ran)
	}
}

// TestWithdrawAndCloseNeedsNoRecord checks that the SIGUSR1 handler still
// takes the banner down and closes the own tab after ks close has trashed
// the record: it finds the tab by the KS_SESSION_ID tag, not the store.
func TestWithdrawAndCloseNeedsNoRecord(t *testing.T) {
	own := session.New("own", "/work/own", 0, 0)
	b, f, _ := newOwnBackend(t, own, true)
	if err := b.l.store.Delete(own.Name); err != nil {
		t.Fatal(err)
	}
	withdrew := false
	b.withdraw = func() { withdrew = true }
	f.calls = nil

	b.WithdrawAndClose()

	if !withdrew {
		t.Error("the banner was not withdrawn")
	}
	if !slices.Contains(f.calls, "CloseTab("+itoa(own.KittyTabID)+")") {
		t.Errorf("own tab not closed with the record trashed: %v", f.calls)
	}
	if f.hasTab(own.KittyTabID) {
		t.Errorf("own tab still in the instance: %+v", f.windows)
	}
}

func TestPokeLooksUntilTheReapFires(t *testing.T) {
	own := session.New("own", "/work/own", 0, 0)
	b, f, _ := newOwnBackend(t, own, true)
	listCalls(t, b, f)

	f.calls, f.slept = nil, 0
	b.Poke() // the agent is still there: every look, nothing closed
	if n := countCalls(f, "Windows"); n != pokeLooks {
		t.Errorf("looks with the agent present = %d, want %d", n, pokeLooks)
	}
	if hasPrefixCall(f, "CloseTab(") {
		t.Errorf("closed a tab with the agent present: %v", f.calls)
	}
	if want := time.Duration(pokeLooks-1) * pokePoll; f.slept != want {
		t.Errorf("slept %v between looks, want %v", f.slept, want)
	}

	dropClaude(f, own)
	f.calls, f.slept = nil, 0
	b.Poke() // gone: the first look reaps and the poke ends
	if n := countCalls(f, "Windows"); n != 1 {
		t.Errorf("looks with the agent gone = %d, want 1: %v", n, f.calls)
	}
	if !hasPrefixCall(f, "CloseTab(") {
		t.Errorf("tab not closed: %v", f.calls)
	}
	if f.slept != 0 {
		t.Errorf("slept %v after the reap, want none", f.slept)
	}

	f.calls = nil
	b.Poke() // reaped already: nothing to look for
	if len(f.calls) != 0 {
		t.Errorf("poke after the reap made calls: %v", f.calls)
	}
}

func TestPokeIgnoresTheHomeSidebar(t *testing.T) {
	b, f, _ := newTestBackend(t)
	f.calls = nil
	b.Poke()
	if len(f.calls) != 0 {
		t.Errorf("home sidebar poke made calls: %v", f.calls)
	}
}

func TestLauncherPoke(t *testing.T) {
	l, f, _ := newTestLauncher(t)
	sess := session.New("s", "/work", 3, 4)
	sess.KittySidebarWindowID = 5
	if err := l.Poke(sess); err != nil {
		t.Fatal(err)
	}
	if want := []string{fmt.Sprintf("SignalTagged(5,%s,SIGUSR2)", sess.ID)}; !slices.Equal(
		f.calls,
		want,
	) {
		t.Errorf("calls = %v, want %v", f.calls, want)
	}

	f.calls = nil
	if err := l.Poke(session.New("bare", "/work", 0, 0)); err != nil || len(f.calls) != 0 {
		t.Errorf("record without a sidebar: err %v, calls %v; want neither", err, f.calls)
	}

	f.errs["SignalTagged"] = errors.New("no matching windows")
	if err := l.Poke(sess); err == nil {
		t.Error("a failed signal was not reported")
	}
}
