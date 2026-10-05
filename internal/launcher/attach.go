package launcher

import (
	"fmt"
	"time"

	"github.com/mad01/kitty-session/internal/session"
)

const (
	// attachStagger separates consecutive claude launches during an attach,
	// so the instance is not hit with every startup at once.
	attachStagger = 100 * time.Millisecond
	// settleAfterLaunch is how long attach waits before checking that the
	// claude windows it launched are still there. A claude with nothing to
	// do (bad flags, missing binary) exits within a second.
	settleAfterLaunch = 2 * time.Second
)

// AttachResult summarizes one attach.
type AttachResult struct {
	// Resumed counts active sessions whose claude window was relaunched and
	// was still there settleAfterLaunch later.
	Resumed int
	// Running counts active sessions that were already alive.
	Running int
	// Stopped counts records the user stopped; attach leaves them alone.
	Stopped int
	// Exited names sessions whose relaunched claude window was gone again
	// settleAfterLaunch later.
	Exited []string
	// Focused is the session brought to the front, "" when it was the home
	// tab because no active session exists or the one to focus failed.
	Focused string
	// Warnings are per-session problems; the attach went on past them.
	Warnings []error
}

// Attach brings the instance back to where the user left it: every active
// session whose claude window is gone is resumed, stopped records are left
// alone, and the most recently focused session (first active by name when
// none was ever focused) ends up in front. With no active session, or when
// that session failed to come back, the home tab is focused.
func (l *Launcher) Attach() (*AttachResult, error) {
	sessions, err := l.store.List()
	if err != nil {
		return nil, err
	}
	res := &AttachResult{}
	var active []*session.Session
	for _, s := range sessions {
		if !s.IsActive() {
			res.Stopped++
			continue
		}
		active = append(active, s)
	}
	target := focusTarget(active)
	skip := l.resume(active, res)
	if target == nil || skip[target.Name] {
		l.focusHome(res)
		return res, nil
	}
	if _, err := l.Open(Request{Name: target.Name, Resume: ResumeStored}); err != nil {
		res.Warnings = append(res.Warnings, fmt.Errorf("could not focus %s: %w", target.Name, err))
		return res, nil
	}
	res.Focused = target.Name
	return res, nil
}

// resume relaunches every active session without a live claude window,
// counting into res, and returns the names attach must not focus: those that
// failed to launch or exited right after.
func (l *Launcher) resume(active []*session.Session, res *AttachResult) map[string]bool {
	skip := map[string]bool{}
	var launched []string
	attempts := 0
	for _, s := range active {
		if l.Alive(s) {
			res.Running++
			continue
		}
		if attempts > 0 {
			l.sleep(attachStagger)
		}
		attempts++
		r, err := l.Open(Request{Name: s.Name, Resume: ResumeStored})
		if err != nil {
			skip[s.Name] = true
			res.Warnings = append(res.Warnings, fmt.Errorf("could not resume %s: %w", s.Name, err))
			continue
		}
		launched = append(launched, s.Name)
		res.Warnings = append(res.Warnings, r.Warnings...)
	}
	for _, name := range l.settle(launched, res) {
		skip[name] = true
	}
	return skip
}

// settle waits for the launched claude processes to get going, then counts
// the ones still there as resumed and reports the rest as exited.
func (l *Launcher) settle(launched []string, res *AttachResult) []string {
	if len(launched) == 0 {
		return nil
	}
	l.sleep(settleAfterLaunch)
	var exited []string
	for _, name := range launched {
		sess, err := l.store.Load(name)
		if err != nil || !l.Alive(sess) {
			exited = append(exited, name)
			continue
		}
		res.Resumed++
	}
	res.Exited = exited
	return exited
}

// focusHome focuses the instance's first window, the home tab's sidebar.
func (l *Launcher) focusHome(res *AttachResult) {
	anchor, err := l.anyWindow()
	if err == nil {
		err = l.kitty.FocusWindow(anchor)
	}
	if err != nil {
		res.Warnings = append(res.Warnings, fmt.Errorf("could not focus the home tab: %w", err))
	}
}

// focusTarget picks the session with the latest FocusedAt; with no stamps
// anywhere that is the first one.
func focusTarget(active []*session.Session) *session.Session {
	var best *session.Session
	for _, s := range active {
		if best == nil || s.FocusedAt.After(best.FocusedAt) {
			best = s
		}
	}
	return best
}
