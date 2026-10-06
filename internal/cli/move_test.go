package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/mad01/kitty-session/internal/launcher"
	"github.com/mad01/kitty-session/internal/session"
)

func TestParseMoveTarget(t *testing.T) {
	tests := []struct {
		in      string
		want    launcher.MoveTarget
		wantErr bool
	}{
		{in: "top", want: launcher.MoveTarget{Kind: launcher.MoveTop}},
		{in: "bottom", want: launcher.MoveTarget{Kind: launcher.MoveBottom}},
		{in: "up", want: launcher.MoveTarget{Kind: launcher.MoveUp}},
		{in: "down", want: launcher.MoveTarget{Kind: launcher.MoveDown}},
		{in: "1", want: launcher.MoveTarget{Kind: launcher.MoveTo, Position: 1}},
		{in: "12", want: launcher.MoveTarget{Kind: launcher.MoveTo, Position: 12}},
		{in: "0", wantErr: true},
		{in: "-1", wantErr: true},
		{in: "Top", wantErr: true},
		{in: "sideways", wantErr: true},
		{in: "", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseMoveTarget(tc.in)
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "top, bottom, up, down") {
					t.Fatalf("err = %v, want one listing the accepted values", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseMoveTarget(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("parseMoveTarget(%q) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestMoveOutsideASession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KS_SESSION_ID", "")
	t.Setenv("KS_SESSION_NAME", "")

	_, err := runCmd(t, "move", "top")
	if !errors.Is(err, errNotInSession) {
		t.Fatalf("move returned %v, want errNotInSession", err)
	}
}

func TestMoveRejectsABadDestinationBeforeAnythingElse(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	_, err := runCmd(t, "move", "demo", "sideways")
	if err == nil || !strings.Contains(err.Error(), `invalid destination "sideways"`) {
		t.Fatalf("move returned %v, want the usage error", err)
	}
}

func TestMoveStoppedSessionNeedsNoInstance(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store, err := session.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	halted := session.New("halted", "/work/halted", 0, 0)
	halted.Status = session.StatusStopped
	if err := store.Save(halted); err != nil {
		t.Fatal(err)
	}

	_, err = runCmd(t, "move", "halted", "top")
	if err == nil || !strings.Contains(err.Error(), `session "halted" has no open tab`) {
		t.Fatalf("move returned %v, want the no-open-tab error", err)
	}
}
