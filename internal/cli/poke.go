package cli

import "github.com/mad01/kitty-session/internal/session"

// hookPoke asks the session's sidebar to look for its agent window now
// (Launcher.Poke), so the tab closes within a moment of the agent's exit
// rather than on the sidebar's next poll. A variable only so the hook tests
// can record the poke instead of reaching for kitty; a poke that fails is
// reported by the hook and the poll still reaps.
var hookPoke = func(sess *session.Session) error {
	w, err := offlineWiring()
	if err != nil {
		return err
	}
	return w.launcher.Poke(sess)
}
