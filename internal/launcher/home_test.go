package launcher

import (
	"slices"
	"strings"
	"testing"

	"github.com/mad01/kitty-session/internal/session"
)

// hasPrefixCall reports whether any recorded call starts with prefix.
func hasPrefixCall(f *fakeKitty, prefix string) bool {
	return slices.ContainsFunc(f.calls, func(c string) bool { return strings.HasPrefix(c, prefix) })
}

func TestOpenRetiresHomeOnceASessionTabExists(t *testing.T) {
	l, f, _ := newTestLauncher(t)
	if _, err := l.Open(Request{Name: "one", Dir: "/work/one"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(f.calls, "CloseTab(1)") || f.find(1) != nil {
		t.Fatalf("home tab kept after the first session: calls %v", f.calls)
	}
	f.calls = nil
	if _, err := l.Open(Request{Name: "two", Dir: "/work/two"}); err != nil {
		t.Fatal(err)
	}
	if hasPrefixCall(f, "CloseTab(") {
		t.Errorf("second open closed a tab: %v", f.calls)
	}
}

func TestOpenKeepsTheAgentHome(t *testing.T) {
	l, f, _ := newTestLauncher(t)
	f.windows[0].HomeAgent = true
	if _, err := l.Open(Request{Name: "one", Dir: "/work/one"}); err != nil {
		t.Fatal(err)
	}
	if hasPrefixCall(f, "CloseTab(") || f.find(1) == nil {
		t.Errorf("home tab running the agent was closed: %v", f.calls)
	}
}

func TestCloseLastSessionRecreatesHomeFirst(t *testing.T) {
	l, f, _ := newTestLauncher(t)
	res, err := l.Open(Request{Name: "one", Dir: "/work/one"})
	if err != nil {
		t.Fatal(err)
	}
	f.calls, f.launches = nil, nil

	warnings, err := l.Close(res.Session, true)
	if err != nil {
		t.Fatal(err)
	}
	// Session one is sidebar 2 and claude 3 in tab 2; the new home is window 4.
	want := []string{"Windows", "LaunchTab", "SetTabTitle(4,ks)", "CloseTab(2)"}
	if !slices.Equal(f.calls, want) {
		t.Errorf("calls =\n%v\nwant\n%v", f.calls, want)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v", warnings)
	}
	if len(f.windows) != 1 || f.windows[0].SessionID != "" || f.windows[0].TabTitle != homeTitle {
		t.Errorf("instance should hold only the home tab, got %+v", f.windows)
	}
	home := f.launches[len(f.launches)-1]
	if want := []string{"/bin/ks", "sidebar"}; !slices.Equal(home.Command, want) {
		t.Errorf("home command = %q, want %q", home.Command, want)
	}
	if len(home.Vars) != 0 {
		t.Errorf("home tab must carry no session tag, got %q", home.Vars)
	}
}

func TestCloseWithOtherSessionsLeavesHomeAlone(t *testing.T) {
	l, f, _ := newTestLauncher(t)
	one, err := l.Open(Request{Name: "one", Dir: "/work/one"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Open(Request{Name: "two", Dir: "/work/two"}); err != nil {
		t.Fatal(err)
	}
	f.calls = nil
	if _, err := l.Close(one.Session, false); err != nil {
		t.Fatal(err)
	}
	if want := []string{"Windows", "CloseTab(2)"}; !slices.Equal(f.calls, want) {
		t.Errorf("calls = %v, want %v", f.calls, want)
	}
}

func TestAttachHomeLifecycle(t *testing.T) {
	t.Run("sessions alive: home closes, attach still ends on the target", func(t *testing.T) {
		l, f, store := newTestLauncher(t)
		var target *session.Session // never focused: attach picks the first by name
		for _, name := range []string{"alpha", "bravo"} {
			sess := session.New(name, "/work/"+name, 0, 0)
			f.addTab(sess, true)
			if err := store.Save(sess); err != nil {
				t.Fatal(err)
			}
			if target == nil {
				target = sess
			}
		}
		if _, err := l.Attach(); err != nil {
			t.Fatal(err)
		}
		if f.find(1) != nil || !slices.Contains(f.calls, "CloseTab(1)") {
			t.Errorf("home tab kept with sessions alive: %v", f.calls)
		}
		want := "FocusWindow(" + itoa(target.KittyWindowID) + ")"
		if last := f.calls[len(f.calls)-1]; last != want {
			t.Errorf("last call = %s, want %s", last, want)
		}
	})

	t.Run("sessions resumed: home closes after the launch", func(t *testing.T) {
		l, f, store := newTestLauncher(t)
		if err := store.Save(session.New("alpha", "/work/alpha", 0, 0)); err != nil {
			t.Fatal(err)
		}
		if _, err := l.Attach(); err != nil {
			t.Fatal(err)
		}
		if f.find(1) != nil {
			t.Errorf("home tab kept after resuming a session: %v", f.calls)
		}
	})

	t.Run("no sessions: home stays and is focused", func(t *testing.T) {
		l, f, _ := newTestLauncher(t)
		if _, err := l.Attach(); err != nil {
			t.Fatal(err)
		}
		if f.find(1) == nil || hasPrefixCall(f, "CloseTab(") {
			t.Errorf("home tab closed with no sessions: %v", f.calls)
		}
		if last := f.calls[len(f.calls)-1]; last != "FocusWindow(1)" {
			t.Errorf("last call = %s, want the home window focused", last)
		}
	})
}
