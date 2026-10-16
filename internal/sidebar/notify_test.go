package sidebar

import (
	"slices"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestEncodeNotification(t *testing.T) {
	tests := []struct {
		name string
		n    Notification
		want string
	}{
		{
			"title and body",
			Notification{
				ID: "ks-ab12", Title: "demo needs input", Body: "~/code/demo", Sound: "question",
			},
			"\x1b]99;i=ks-ab12:p=title:d=0:e=1:o=invisible:u=1:s=cXVlc3Rpb24=:f=a3M=;" +
				"ZGVtbyBuZWVkcyBpbnB1dA==\x1b\\" +
				"\x1b]99;i=ks-ab12:p=body:d=1:e=1;fi9jb2RlL2RlbW8=\x1b\\",
		},
		{
			"title alone is displayed in one chunk",
			Notification{ID: "ks-ab12", Title: "demo finished", Sound: "info"},
			"\x1b]99;i=ks-ab12:p=title:d=1:e=1:o=invisible:u=1:s=aW5mbw==:f=a3M=;" +
				"ZGVtbyBmaW5pc2hlZA==\x1b\\",
		},
		{
			"a colon and a non-ASCII rune in the title stay out of the metadata",
			Notification{ID: "ks-1", Title: "a:b ü", Body: "Fix the build", Sound: "question"},
			"\x1b]99;i=ks-1:p=title:d=0:e=1:o=invisible:u=1:s=cXVlc3Rpb24=:f=a3M=;YTpiIMO8\x1b\\" +
				"\x1b]99;i=ks-1:p=body:d=1:e=1;Rml4IHRoZSBidWlsZA==\x1b\\",
		},
		{
			"no sound leaves kitty's default",
			Notification{ID: "ks-1", Title: "demo finished"},
			"\x1b]99;i=ks-1:p=title:d=1:e=1:o=invisible:u=1:f=a3M=;ZGVtbyBmaW5pc2hlZA==\x1b\\",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(encodeNotification(tc.n)); got != tc.want {
				t.Errorf("encodeNotification =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

func TestEncodeClose(t *testing.T) {
	want := "\x1b]99;i=ks-ab12:p=close;\x1b\\"
	if got := string(encodeClose("ks-ab12")); got != want {
		t.Errorf("encodeClose = %q, want %q", got, want)
	}
}

// fakeNotifier records every call in order.
type fakeNotifier struct {
	calls []string
	notes []Notification
}

func (f *fakeNotifier) Notify(n Notification) error {
	f.notes = append(f.notes, n)
	f.calls = append(f.calls, "notify:"+n.Title)
	return nil
}

func (f *fakeNotifier) Close(id string) error {
	f.calls = append(f.calls, "close:"+id)
	return nil
}

// ownRow is the row of the session the test sidebar sits in, at st.
func ownRow(st State) Agent {
	return Agent{Name: "demo", Dir: "/home/u/code/demo", State: st, Tab: 1, Own: true}
}

// foreignRow is another session's row, at st.
func foreignRow(st State) Agent {
	return Agent{Name: "other", Dir: "/home/u/code/other", State: st, Tab: 2}
}

// newNotifyModel returns a sized model over fb that notifies through fn for
// the states in notify, before any List has run.
func newNotifyModel(t *testing.T, fb *fakeBackend, fn *fakeNotifier, notify []string) model {
	t.Helper()
	m := newModel(Options{
		Session: "demo", SessionID: "sid-1", Width: DefaultWidth, Backend: fb,
		Notify: notify, Notifier: fn,
	}, "/home/u")
	m.now = func() time.Time { return testNow }
	return update(t, m, tea.WindowSizeMsg{Width: testCols, Height: testLines})
}

// refresh runs a List, applies it and runs the notification command the
// refresh produced, the way the program would off the update loop.
func refresh(t *testing.T, m model) model {
	t.Helper()
	cmd := m.listCmd()
	next, notify := m.Update(cmd())
	m = asModel(t, next)
	m, _ = feed(t, m, notify)
	return m
}

func wantNotifyCalls(t *testing.T, fn *fakeNotifier, want ...string) {
	t.Helper()
	if !slices.Equal(fn.calls, want) {
		t.Fatalf("notifier calls = %q, want %q", fn.calls, want)
	}
}

func TestOwnStateTransitionsNotify(t *testing.T) {
	both := []string{"input", "done"}
	input := []string{"input"}
	tests := []struct {
		name   string
		notify []string
		states []State // the own row's state on each successive refresh
		want   []string
	}{
		{
			"first load at input raises", input,
			[]State{StateInput},
			[]string{"notify:demo needs input"},
		},
		{
			"input then working takes it down", input,
			[]State{StateInput, StateWorking},
			[]string{"notify:demo needs input", "close:ks-sid-1"},
		},
		{
			"input then input is quiet", input,
			[]State{StateInput, StateInput},
			[]string{"notify:demo needs input"},
		},
		{
			"idle then done raises finished", both,
			[]State{StateIdle, StateDone},
			[]string{"notify:demo finished"},
		},
		{"done is quiet unless enabled", input, []State{StateIdle, StateDone}, nil},
		{
			"input then done replaces, no close between", both,
			[]State{StateInput, StateDone},
			[]string{"notify:demo needs input", "notify:demo finished"},
		},
		{
			"done then idle takes it down", both,
			[]State{StateDone, StateIdle},
			[]string{"notify:demo finished", "close:ks-sid-1"},
		},
		{"working then idle is quiet", both, []State{StateWorking, StateIdle}, nil},
		{"nothing enabled is quiet", nil, []State{StateInput, StateWorking}, nil},
		{"error is not notified", both, []State{StateInput, StateError}, []string{
			"notify:demo needs input", "close:ks-sid-1",
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fb := &fakeBackend{}
			fn := &fakeNotifier{}
			m := newNotifyModel(t, fb, fn, tc.notify)
			for _, st := range tc.states {
				fb.agents = []Agent{ownRow(st), foreignRow(StateIdle)}
				m = refresh(t, m)
			}
			wantNotifyCalls(t, fn, tc.want...)
		})
	}
}

func TestNotificationPayload(t *testing.T) {
	tests := []struct {
		name string
		row  Agent
		want Notification
	}{
		{
			"input without a title names the directory",
			ownRow(StateInput),
			Notification{
				ID: "ks-sid-1", Title: "demo needs input", Body: "~/code/demo", Sound: "question",
			},
		},
		{
			"input with a title carries it",
			func() Agent { a := ownRow(StateInput); a.Title = "Fix the build"; return a }(),
			Notification{
				ID: "ks-sid-1", Title: "demo needs input", Body: "Fix the build", Sound: "question",
			},
		},
		{
			"done",
			ownRow(StateDone),
			Notification{
				ID:    "ks-sid-1",
				Title: "demo finished",
				Body:  "~/code/demo",
				Sound: "info",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fb := &fakeBackend{agents: []Agent{tc.row}}
			fn := &fakeNotifier{}
			refresh(t, newNotifyModel(t, fb, fn, []string{"input", "done"}))
			if len(fn.notes) != 1 || fn.notes[0] != tc.want {
				t.Fatalf("notified %+v, want %+v", fn.notes, tc.want)
			}
		})
	}
}

func TestForeignRowsNeverNotify(t *testing.T) {
	fb := &fakeBackend{agents: []Agent{ownRow(StateIdle), foreignRow(StateInput)}}
	fn := &fakeNotifier{}
	m := refresh(t, newNotifyModel(t, fb, fn, []string{"input", "done"}))
	fb.agents = []Agent{ownRow(StateIdle), foreignRow(StateDone)}
	m = refresh(t, m)
	fb.agents = []Agent{ownRow(StateIdle), foreignRow(StateWorking)}
	refresh(t, m)
	wantNotifyCalls(t, fn)
}

// The home tab's sidebar has no own row, so it never speaks, whatever the
// sessions do.
func TestHomeSidebarNeverNotifies(t *testing.T) {
	fb := &fakeBackend{agents: []Agent{foreignRow(StateInput)}}
	fn := &fakeNotifier{}
	m := refresh(t, newNotifyModel(t, fb, fn, []string{"input", "done"}))
	fb.agents = []Agent{foreignRow(StateDone)}
	refresh(t, m)
	wantNotifyCalls(t, fn)
}

// An own row that disappears takes its notification with it, and one that
// comes back at input counts as entering input again.
func TestOwnRowGoneClosesAndBackRaises(t *testing.T) {
	fb := &fakeBackend{agents: []Agent{ownRow(StateInput)}}
	fn := &fakeNotifier{}
	m := refresh(t, newNotifyModel(t, fb, fn, []string{"input"}))
	fb.agents = []Agent{foreignRow(StateIdle)}
	m = refresh(t, m)
	fb.agents = []Agent{ownRow(StateInput)}
	refresh(t, m)
	wantNotifyCalls(t, fn, "notify:demo needs input", "close:ks-sid-1", "notify:demo needs input")
}

// The identifier comes from the session id, so a rename between two
// refreshes neither raises a second notification nor strands the first.
func TestRenamedOwnRowKeepsItsID(t *testing.T) {
	fb := &fakeBackend{agents: []Agent{ownRow(StateInput)}}
	fn := &fakeNotifier{}
	m := refresh(t, newNotifyModel(t, fb, fn, []string{"input"}))
	renamed := ownRow(StateInput)
	renamed.Name = "demo-2"
	fb.agents = []Agent{renamed}
	m = refresh(t, m)
	renamed.State = StateWorking
	fb.agents = []Agent{renamed}
	refresh(t, m)
	wantNotifyCalls(t, fn, "notify:demo needs input", "close:ks-sid-1")
}

// A notifier failure lands on the status line like any other action's.
func TestNotifierErrorShowsOnStatusLine(t *testing.T) {
	fb := &fakeBackend{agents: []Agent{ownRow(StateInput)}}
	m := newModel(Options{
		Session: "demo", SessionID: "sid-1", Width: DefaultWidth, Backend: fb,
		Notify: []string{"input"}, Notifier: failingNotifier{},
	}, "/home/u")
	m = update(t, m, tea.WindowSizeMsg{Width: testCols, Height: testLines})
	if m.status != "" {
		t.Fatalf("status = %q before any refresh", m.status)
	}
	m = refresh(t, m)
	if m.status != "fake failure" || !m.statusErr {
		t.Fatalf("status = %q (err %v), want the notifier's failure", m.status, m.statusErr)
	}
}

type failingNotifier struct{}

func (failingNotifier) Notify(Notification) error { return errFake }
func (failingNotifier) Close(string) error        { return errFake }

// With no Notifier and nothing enabled the model runs on the no-op one.
func TestNilNotifierIsNoop(t *testing.T) {
	m := newTestModel(t, &fakeBackend{agents: mockupAgents()}, "")
	if _, ok := m.notifier.(noopNotifier); !ok {
		t.Fatalf("notifier = %T, want noopNotifier", m.notifier)
	}
}

// closeOrder is a Notifier that records each Close with how many backend
// calls had been made by then.
type closeOrder struct {
	fb     *fakeBackend
	ids    []string
	before int
}

func (c *closeOrder) Notify(Notification) error { return nil }

func (c *closeOrder) Close(id string) error {
	c.ids = append(c.ids, id)
	c.before = len(c.fb.calls)
	return nil
}

// TestCloseActionWithdrawsOwnNotificationFirst checks that closing or
// deleting the own session from its sidebar takes its notification down
// before the backend closes the tab, and that another session's row leaves
// the notifier alone.
func TestCloseActionWithdrawsOwnNotificationFirst(t *testing.T) {
	fb := &fakeBackend{agents: []Agent{ownRow(StateInput), foreignRow(StateInput)}}
	co := &closeOrder{fb: fb}
	m := newModel(Options{
		Session: "demo", SessionID: "sid-1", Width: DefaultWidth, Backend: fb,
		Notify: []string{"input"}, Notifier: co,
	}, "/home/u")
	m = update(t, m, tea.WindowSizeMsg{Width: testCols, Height: testLines})
	m = refresh(t, m) // the cursor sits on the own row

	m, _ = run(t, m, "c", "y")
	wantCalls(t, fb, "close:demo:true")
	if !slices.Equal(co.ids, []string{"ks-sid-1"}) || co.before != 0 {
		t.Fatalf("Close ids %q after %d backend calls; want ks-sid-1 before any", co.ids, co.before)
	}

	co.ids = nil
	m, _ = press(t, m, "j") // the other session
	run(t, m, "d", "y")
	wantCalls(t, fb, "close:demo:true", "close:other:false")
	if co.ids != nil {
		t.Errorf("deleting another session withdrew %q", co.ids)
	}
}
