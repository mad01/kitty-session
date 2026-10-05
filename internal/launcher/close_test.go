package launcher

import (
	"errors"
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
	store := newTestStore(t)
	tests := []struct {
		name      string
		session   string
		keep      bool
		tabAlive  bool
		closeErr  error
		wantCalls []string
		wantWarns int
	}{
		{
			name:     "keep marks stopped and closes live tabs",
			session:  "kept",
			keep:     true,
			tabAlive: true,
			wantCalls: []string{
				"TabExists", "CloseTab", "WindowExists", "CloseTabForWindow",
				"WindowExists", "CloseTabForWindow",
			},
		},
		{
			name:      "delete trashes the record and skips dead tabs",
			session:   "deleted",
			tabAlive:  false,
			wantCalls: []string{"TabExists", "WindowExists", "WindowExists"},
		},
		{
			name:      "tabs that will not close are warnings",
			session:   "stubborn",
			keep:      true,
			tabAlive:  true,
			closeErr:  errors.New("kitty says no"),
			wantWarns: 3,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sess := session.New(tc.session, "/work/demo", 3, 9)
			sess.KittyShellWindowID, sess.KittySummaryWindowID = 5, 6
			if err := store.Save(sess); err != nil {
				t.Fatal(err)
			}
			if err := state.Write(tc.session, "idle"); err != nil {
				t.Fatal(err)
			}
			b := &fakeBackend{
				tabAlive:    tc.tabAlive,
				windowAlive: tc.tabAlive,
				closeErr:    tc.closeErr,
			}

			warnings, err := closeWith(store, b, sess, tc.keep)
			if err != nil {
				t.Fatalf("closeWith: %v", err)
			}
			if len(warnings) != tc.wantWarns {
				t.Errorf("warnings = %v, want %d", warnings, tc.wantWarns)
			}
			if tc.wantCalls != nil && !slices.Equal(b.calls, tc.wantCalls) {
				t.Errorf("calls = %v, want %v", b.calls, tc.wantCalls)
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
