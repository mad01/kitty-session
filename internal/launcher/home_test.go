package launcher

import (
	"slices"
	"strings"
	"testing"

	"github.com/mad01/kitty-session/internal/kitty"
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
	if !f.windows[0].Home {
		t.Errorf("recreated home tab is untagged: %+v", f.windows[0])
	}
	home := f.launches[len(f.launches)-1]
	if want := []string{"/bin/ks", "sidebar"}; !slices.Equal(home.Command, want) {
		t.Errorf("home command = %q, want %q", home.Command, want)
	}
	if want := []string{kitty.HomeVar + "=1"}; !slices.Equal(home.Vars, want) {
		t.Errorf("home tab vars = %q, want %q", home.Vars, want)
	}
}

func TestCloseLastSessionBesideAUserTabMakesNoHome(t *testing.T) {
	l, f, _ := newTestLauncher(t)
	res, err := l.Open(Request{Name: "one", Dir: "/work/one"})
	if err != nil {
		t.Fatal(err)
	}
	user := f.addUserTab()
	f.calls = nil

	warnings, err := l.Close(res.Session, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v", warnings)
	}
	// The user's tab holds the instance up, so only the session tab goes.
	if want := []string{"Windows", "CloseTab(2)"}; !slices.Equal(f.calls, want) {
		t.Errorf("calls = %v, want %v", f.calls, want)
	}
	if len(f.windows) != 1 || !f.hasTab(user) {
		t.Errorf("instance should hold only the user's tab %d, got %+v", user, f.windows)
	}
}

func TestOpenLeavesUserTabsAlone(t *testing.T) {
	t.Run("a new tab retires the home tab, not the user's", func(t *testing.T) {
		l, f, _ := newTestLauncher(t)
		user := f.addUserTab()
		if _, err := l.Open(Request{Name: "one", Dir: "/work/one"}); err != nil {
			t.Fatal(err)
		}
		if f.find(1) != nil {
			t.Errorf("home tab kept: %v", f.calls)
		}
		if !f.hasTab(user) {
			t.Errorf("user tab %d closed: %v", user, f.calls)
		}
	})

	t.Run("focusing a live session closes nothing", func(t *testing.T) {
		l, f, _ := newTestLauncher(t)
		if _, err := l.Open(Request{Name: "one", Dir: "/work/one"}); err != nil {
			t.Fatal(err)
		}
		user := f.addUserTab()
		f.calls = nil
		res, err := l.Open(Request{Name: "one", Resume: ResumeStored})
		if err != nil {
			t.Fatal(err)
		}
		if !res.Focused {
			t.Fatal("live session was relaunched, want focused")
		}
		if hasPrefixCall(f, "CloseTab(") || !f.hasTab(user) {
			t.Errorf("user tab %d closed on focus: %v", user, f.calls)
		}
	})

	t.Run("relaunching a dead session closes nothing", func(t *testing.T) {
		l, f, store := newTestLauncher(t)
		sess := session.New("one", "/work/one", 70, 71) // stale ids: nothing in the instance
		if err := store.Save(sess); err != nil {
			t.Fatal(err)
		}
		user := f.addUserTab()
		f.calls = nil
		if _, err := l.Open(Request{Name: "one", Resume: ResumeStored}); err != nil {
			t.Fatal(err)
		}
		if !f.hasTab(user) {
			t.Errorf("user tab %d closed on relaunch: %v", user, f.calls)
		}
		if f.find(1) != nil {
			t.Errorf("home tab kept after the relaunch: %v", f.calls)
		}
	})
}

// An instance started by an older ks has an untagged home tab. It looks like
// a user tab and is left alone; no migration tags it.
func TestUntaggedHomeIsTreatedAsTheUsers(t *testing.T) {
	l, f, _ := newTestLauncher(t)
	f.windows[0].Home = false
	if _, err := l.Open(Request{Name: "one", Dir: "/work/one"}); err != nil {
		t.Fatal(err)
	}
	if hasPrefixCall(f, "CloseTab(") || f.find(1) == nil {
		t.Errorf("untagged first tab was closed: %v", f.calls)
	}
}

func TestHomeTabs(t *testing.T) {
	home := kitty.Window{ID: 1, TabID: 1, Home: true}
	user := kitty.Window{ID: 2, TabID: 2}
	sidebar := kitty.Window{ID: 3, TabID: 3, SessionID: "abc"}
	claude := kitty.Window{ID: 4, TabID: 3, SessionID: "abc"}
	again := kitty.Window{ID: 5, TabID: 4, Home: true}
	tests := []struct {
		name string
		all  []kitty.Window
		want []int
	}{
		{name: "the tagged home tab", all: []kitty.Window{home}, want: []int{1}},
		{name: "a user tab is not home", all: []kitty.Window{home, user}, want: []int{1}},
		{name: "a session tab is not home", all: []kitty.Window{sidebar, claude, user}},
		{name: "two home tabs, in instance order", all: []kitty.Window{sidebar, again, claude, home}, want: []int{4, 1}},
		{name: "empty instance", all: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := homeTabs(tc.all); !slices.Equal(got, tc.want) {
				t.Errorf("homeTabs = %v, want %v", got, tc.want)
			}
		})
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

	t.Run("sessions alive: the user's tab survives", func(t *testing.T) {
		l, f, store := newTestLauncher(t)
		sess := session.New("alpha", "/work/alpha", 0, 0)
		f.addTab(sess, true)
		if err := store.Save(sess); err != nil {
			t.Fatal(err)
		}
		user := f.addUserTab()
		if _, err := l.Attach(); err != nil {
			t.Fatal(err)
		}
		if f.find(1) != nil {
			t.Errorf("home tab kept with a session alive: %v", f.calls)
		}
		if !f.hasTab(user) {
			t.Errorf("user tab %d closed by attach: %v", user, f.calls)
		}
	})

	t.Run("no sessions: the home sidebar is focused behind a user tab", func(t *testing.T) {
		l, f, _ := newTestLauncher(t)
		f.windows = []kitty.Window{
			{ID: 1, TabID: 1, TabTitle: "zsh", Title: "zsh", Columns: fakeTabColumns},
			{ID: 2, TabID: 2, TabTitle: "ks", Title: "ks", Columns: fakeTabColumns, Home: true},
		}
		f.nextWindow, f.nextTab = 3, 3
		if _, err := l.Attach(); err != nil {
			t.Fatal(err)
		}
		if hasPrefixCall(f, "CloseTab(") || len(f.windows) != 2 {
			t.Errorf("a tab was closed with no sessions: %v", f.calls)
		}
		if last := f.calls[len(f.calls)-1]; last != "FocusWindow(2)" {
			t.Errorf("last call = %s, want the home sidebar focused, not the user's tab", last)
		}
	})
}
