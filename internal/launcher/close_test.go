package launcher

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/mad01/kitty-session/internal/session"
	"github.com/mad01/kitty-session/internal/state"
)

// TestClose is the only test in this package that touches state files:
// state.Dir pins HOME on first use, so every case shares one HOME and store
// and a second test with its own HOME would write into a removed directory.
func TestClose(t *testing.T) {
	l, f, store := newTestLauncher(t)
	tests := []struct {
		name      string
		session   string
		keep      bool
		tabAlive  bool
		closeErr  error
		lsErr     error
		wantClose bool // a CloseTab call is expected
		wantWarns int
	}{
		{
			name:      "keep marks stopped and closes the live tab",
			session:   "kept",
			keep:      true,
			tabAlive:  true,
			wantClose: true,
		},
		{
			name:    "delete trashes the record and skips a dead tab",
			session: "deleted",
		},
		{
			name:      "a tab that will not close is a warning",
			session:   "stubborn",
			keep:      true,
			tabAlive:  true,
			closeErr:  errors.New("kitty says no"),
			wantClose: true,
			wantWarns: 1,
		},
		{
			name:      "an unreachable instance is a warning, the record still stops",
			session:   "offline",
			keep:      true,
			tabAlive:  true,
			lsErr:     errors.New("connection refused"),
			wantWarns: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sess := session.New(tc.session, "/work/demo", 0, 0)
			if tc.tabAlive {
				f.addTab(sess, true)
			} else {
				sess.KittyTabID, sess.KittyWindowID = 70, 71 // stale ids, nothing in kitty
			}
			if err := store.Save(sess); err != nil {
				t.Fatal(err)
			}
			if err := state.Write(tc.session, "idle"); err != nil {
				t.Fatal(err)
			}
			f.calls = nil
			f.errs["CloseTab"], f.errs["Windows"] = tc.closeErr, tc.lsErr

			warnings, err := l.Close(sess, tc.keep)
			if err != nil {
				t.Fatalf("Close: %v", err)
			}
			if len(warnings) != tc.wantWarns {
				t.Errorf("warnings = %v, want %d", warnings, tc.wantWarns)
			}
			wantCalls := []string{"Windows"}
			if tc.wantClose {
				wantCalls = append(wantCalls, fmt.Sprintf("CloseTab(%d)", sess.KittyTabID))
			}
			if !slices.Equal(f.calls, wantCalls) {
				t.Errorf("calls = %v, want %v", f.calls, wantCalls)
			}
			if _, _, err := state.Read(tc.session); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("state file still present (err %v), want removed", err)
			}

			got, err := store.Load(tc.session)
			if !tc.keep {
				if err == nil {
					t.Fatal("record still in the store, want trashed")
				}
				trashed, err := store.ListTrashed()
				if err != nil || len(trashed) != 1 || trashed[0].Name != tc.session {
					t.Errorf("trash = %v, %v; want just %s", trashed, err, tc.session)
				}
				return
			}
			if err != nil {
				t.Fatalf("kept record missing: %v", err)
			}
			if got.Status != session.StatusStopped {
				t.Errorf("Status = %q, want stopped", got.Status)
			}
			if got.ClaudeSessionID != sess.ClaudeSessionID || got.ID != sess.ID {
				t.Errorf("record fields changed on close: %+v", got)
			}
		})
	}
}
