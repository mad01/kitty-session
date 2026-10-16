package sidebar

import (
	"encoding/base64"
	"slices"

	tea "github.com/charmbracelet/bubbletea"
)

// Notification is one desktop notification the sidebar raises about its own
// session. ID is kitty's notification identifier: a later Notification with
// the same ID replaces the one on screen, and Close takes it down.
type Notification struct {
	ID    string
	Title string
	Body  string
	// Sound is the name kitty plays: question, info, silent and the others
	// its protocol lists. Empty leaves kitty's default.
	Sound string
}

// Notifier raises and takes down desktop notifications. The production one
// speaks kitty's protocol through the sidebar's terminal; tests record calls.
type Notifier interface {
	Notify(n Notification) error
	Close(id string) error
}

// emitter writes one escape sequence to the terminal between frames.
type emitter interface {
	emit(p []byte) error
}

// OSCNotifier speaks kitty's desktop notification protocol (OSC 99,
// https://sw.kovidgoyal.net/kitty/desktop-notifications/) through the
// sidebar's own terminal, so the notification is tied to the sidebar's
// window: kitty shows it only while that window is off screen or its OS
// window is inactive, and a click on it focuses the window.
type OSCNotifier struct {
	out emitter
}

// NewOSCNotifier returns a notifier writing through out, the same writer
// the sidebar's frames go to, so a notification never lands inside one.
func NewOSCNotifier(out *SyncOutput) *OSCNotifier {
	return &OSCNotifier{out: out}
}

// Notify raises n, replacing any notification with the same ID.
func (o *OSCNotifier) Notify(n Notification) error {
	return o.out.emit(encodeNotification(n))
}

// Close takes down the notification with the given ID.
func (o *OSCNotifier) Close(id string) error {
	return o.out.emit(encodeClose(id))
}

// NotifyID is the kitty identifier of a session's notification, from the
// session's id, so a rename does not leave a stale one behind.
func NotifyID(sessionID string) string {
	return "ks-" + sessionID
}

// noopNotifier raises nothing: for the demo backend, for a sidebar with
// notifications off, and for tests that are not about them.
type noopNotifier struct{}

func (noopNotifier) Notify(Notification) error { return nil }
func (noopNotifier) Close(string) error        { return nil }

// The OSC 99 framing: ESC ] 99 ; <metadata> ; <payload> ESC \.
const (
	oscStart = "\x1b]99;"
	oscEnd   = "\x1b\\"
	// appName is what the notification is shown as coming from.
	appName = "ks"
)

// encodeNotification is the OSC 99 sequence for n: a title chunk carrying
// the metadata, then a body chunk when there is a body. Every text field is
// base64 (e=1), so a colon or a non-ASCII rune in the title cannot break the
// metadata. o=invisible asks kitty to show it only when the sending window
// is not on screen; u=1 is normal urgency.
func encodeNotification(n Notification) []byte {
	done := "1"
	if n.Body != "" {
		done = "0"
	}
	meta := "i=" + n.ID + ":p=title:d=" + done + ":e=1:o=invisible:u=1"
	if n.Sound != "" {
		meta += ":s=" + b64(n.Sound)
	}
	meta += ":f=" + b64(appName)
	seq := oscStart + meta + ";" + b64(n.Title) + oscEnd
	if n.Body != "" {
		seq += oscStart + "i=" + n.ID + ":p=body:d=1:e=1;" + b64(n.Body) + oscEnd
	}
	return []byte(seq)
}

// encodeClose is the OSC 99 sequence that takes down the notification id.
func encodeClose(id string) []byte {
	return []byte(oscStart + "i=" + id + ":p=close;" + oscEnd)
}

func b64(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// notifies reports whether the sidebar raises a notification when its own
// session enters st: Options.Notify names the states by their labels.
func (m model) notifies(st State) bool {
	if st != StateInput && st != StateDone {
		return false
	}
	return slices.Contains(m.opts.Notify, st.String())
}

// ownNotifyCmd compares the own row after a refresh with the one before and
// returns the command that raises or takes down the session's notification,
// nil when nothing changed. Entering input or done raises one when that
// state is enabled; leaving it, or the own row going away, takes it down.
// A first refresh that already shows the state counts as entering it. The
// one identifier per session means a raise replaces whatever is showing, so
// no Close precedes it.
func (m *model) ownNotifyCmd() tea.Cmd {
	own, ok := m.ownAgent()
	prev, seen := m.lastOwn, m.ownSeen
	m.ownSeen = ok
	if !ok {
		if seen && m.notifies(prev) {
			return m.closeCmd()
		}
		return nil
	}
	m.lastOwn = own.State
	if seen && own.State == prev {
		return nil
	}
	if m.notifies(own.State) {
		return m.notifyCmd(own)
	}
	if seen && m.notifies(prev) {
		return m.closeCmd()
	}
	return nil
}

// ownAgent returns the Own row among every agent listed, filter or not.
func (m model) ownAgent() (Agent, bool) {
	for _, a := range m.agents {
		if a.Own {
			return a, true
		}
	}
	return Agent{}, false
}

// notifyID is the kitty identifier of this session's notification.
func (m model) notifyID() string {
	return NotifyID(m.opts.SessionID)
}

// notifyCmd raises the notification for own's state off the update loop.
func (m model) notifyCmd(own Agent) tea.Cmd {
	n := Notification{ID: m.notifyID(), Body: own.Title}
	if n.Body == "" {
		n.Body = ShortenHome(own.Dir, m.home)
	}
	switch own.State {
	case StateInput:
		n.Title, n.Sound = own.Name+" needs input", "question"
	case StateDone:
		n.Title, n.Sound = own.Name+" finished", "info"
	}
	notifier := m.notifier
	return act(func() error { return notifier.Notify(n) }, doneMsg{})
}

// closeCmd takes the session's notification down off the update loop.
func (m model) closeCmd() tea.Cmd {
	notifier, id := m.notifier, m.notifyID()
	return act(func() error { return notifier.Close(id) }, doneMsg{})
}
