package cli

import (
	"strings"
	"testing"

	"github.com/mad01/kitty-session/internal/session"
)

func TestSelfSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store, err := session.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	demo := session.New("demo", "/work/demo", 0, 0)
	legacy := &session.Session{Name: "legacy", Dir: "/work/legacy"} // predates ids
	for _, sess := range []*session.Session{demo, legacy} {
		if err := store.Save(sess); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		name    string
		id      string // KS_SESSION_ID
		session string // KS_SESSION_NAME
		want    string // the record found
		wantErr string
	}{
		{name: "the id wins over a stale name", id: demo.ID, session: "stale", want: "demo"},
		{name: "the id alone", id: demo.ID, want: "demo"},
		{name: "the name finds a record without an id", session: "legacy", want: "legacy"},
		{name: "an unknown id falls back to the name", id: "nobody", session: "legacy", want: "legacy"},
		{name: "outside a session", wantErr: errNotInSession.Error()},
		{
			name: "a record that is gone", id: "nobody", session: "gone",
			wantErr: "cannot find the session this shell runs in",
		},
		{name: "an unknown id and no name", id: "nobody", wantErr: `session with id "nobody" not found`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("KS_SESSION_ID", tc.id)
			t.Setenv("KS_SESSION_NAME", tc.session)

			sess, err := selfSession(store)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("selfSession: %v", err)
			}
			if sess.Name != tc.want {
				t.Errorf("session = %q, want %q", sess.Name, tc.want)
			}
		})
	}
}

func TestNameOrSelf(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store, err := session.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	demo := session.New("demo", "/work/demo", 0, 0)
	if err := store.Save(demo); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KS_SESSION_ID", demo.ID)
	t.Setenv("KS_SESSION_NAME", "demo")

	if name, err := nameOrSelf(store, []string{"other"}); err != nil || name != "other" {
		t.Errorf("nameOrSelf(other) = %q, %v; want other", name, err)
	}
	if name, err := nameOrSelf(store, nil); err != nil || name != "demo" {
		t.Errorf("nameOrSelf() = %q, %v; want demo", name, err)
	}
}
