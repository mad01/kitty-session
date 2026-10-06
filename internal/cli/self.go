package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/mad01/kitty-session/internal/session"
)

// errNotInSession is what the one-argument forms of rename and move report
// when the environment names no session.
var errNotInSession = errors.New("not inside a ks session: pass the session name")

// findSession resolves the record the launcher's environment points at: by
// KS_SESSION_ID when there is one, since KS_SESSION_NAME goes stale after a
// rename, then by KS_SESSION_NAME for records that predate ids. The hook and
// the self forms of rename and move share it.
func findSession(store *session.Store, id, name string) (*session.Session, error) {
	if id == "" {
		return store.Load(name)
	}
	sess, err := store.FindByID(id)
	if err == nil || name == "" {
		return sess, err
	}
	return store.Load(name)
}

// selfSession returns the record of the session this process runs in, from
// the KS_SESSION_ID and KS_SESSION_NAME the launcher exports into both
// windows of a session. Outside a session it returns errNotInSession.
func selfSession(store *session.Store) (*session.Session, error) {
	id, name := os.Getenv("KS_SESSION_ID"), os.Getenv("KS_SESSION_NAME")
	if id == "" && name == "" {
		return nil, errNotInSession
	}
	sess, err := findSession(store, id, name)
	if err != nil {
		return nil, fmt.Errorf("cannot find the session this shell runs in: %w", err)
	}
	return sess, nil
}

// nameOrSelf returns the session a command works on: the name given, or the
// session this process runs in when the name was left out.
func nameOrSelf(store *session.Store, args []string) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}
	sess, err := selfSession(store)
	if err != nil {
		return "", err
	}
	return sess.Name, nil
}
