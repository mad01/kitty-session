package launcher

import (
	"errors"
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

func TestReapLeavesTheRecordAlone(t *testing.T) {
	own := session.New("own", "/work/own", 0, 0)
	b, f, _ := newOwnBackend(t, own, true)
	listCalls(t, b, f)
	dropClaude(f, own)
	if calls := listCalls(t, b, f); !hasPrefixCall(f, "CloseTab(") {
		t.Fatalf("tab not closed: %v", calls)
	}
	got, err := b.l.store.Load("own")
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsActive() || got.ID != own.ID || got.KittyTabID != own.KittyTabID {
		t.Errorf("record changed by the reap: %+v", got)
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
