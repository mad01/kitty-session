package launcher

import (
	"errors"
	"slices"
	"testing"

	"github.com/mad01/kitty-session/internal/session"
)

func TestRename(t *testing.T) {
	tests := []struct {
		name      string
		tabAlive  bool
		lsErr     error
		wantCalls []string
		wantWarns int
	}{
		{
			name:      "live tab is retitled through its sidebar",
			tabAlive:  true,
			wantCalls: []string{"Windows", "SetTabTitle(2,new)"},
		},
		{
			name:      "no live tab, nothing to retitle",
			wantCalls: []string{"Windows"},
		},
		{
			name:      "unreachable instance is a warning, the rename stands",
			tabAlive:  true,
			lsErr:     errors.New("connection refused"),
			wantCalls: []string{"Windows"},
			wantWarns: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l, f, store := newTestLauncher(t)
			sess := session.New("old", "/work/demo", 0, 0)
			if tc.tabAlive {
				f.addTab(sess, true)
			}
			if err := store.Save(sess); err != nil {
				t.Fatal(err)
			}
			f.errs["Windows"] = tc.lsErr

			got, warnings, err := l.Rename("old", "new")
			if err != nil {
				t.Fatalf("Rename: %v", err)
			}
			if got.Name != "new" || got.ID != sess.ID {
				t.Errorf("renamed record = %+v, want name new with the same id", got)
			}
			if len(warnings) != tc.wantWarns {
				t.Errorf("warnings = %v, want %d", warnings, tc.wantWarns)
			}
			if !slices.Equal(f.calls, tc.wantCalls) {
				t.Errorf("calls = %v, want %v", f.calls, tc.wantCalls)
			}
			if store.Exists("old") || !store.Exists("new") {
				t.Error("store still has old or lacks new")
			}
			if tc.tabAlive && tc.lsErr == nil {
				if w := f.find(sess.KittySidebarWindowID); w == nil || w.TabTitle != "new" {
					t.Errorf("tab title = %+v, want new", w)
				}
			}
		})
	}
}

func TestRenameTakenName(t *testing.T) {
	l, f, store := newTestLauncher(t)
	for _, name := range []string{"a", "b"} {
		if err := store.Save(session.New(name, "/work/"+name, 0, 0)); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := l.Rename("a", "b"); err == nil {
		t.Fatal("renaming onto a taken name succeeded")
	}
	if len(f.calls) != 0 {
		t.Errorf("kitty driven for a rejected rename: %v", f.calls)
	}
	if !store.Exists("a") {
		t.Error("record a lost")
	}
}
