package sidebar

import (
	"slices"
	"testing"

	"github.com/mad01/kitty-session/internal/session"
)

// The chooser's list is a copy of session.Kinds so the package needs no
// session import; this keeps the copy honest.
func TestKindOptionsMatchSessionKinds(t *testing.T) {
	if !slices.Equal(kindOptions, session.Kinds) {
		t.Fatalf("kindOptions = %v, session.Kinds = %v", kindOptions, session.Kinds)
	}
}

func TestKindBadge(t *testing.T) {
	cases := map[string]string{"": "", "claude": "", "pi": "pi", "shell": "sh"}
	for kind, want := range cases {
		if got := kindBadge(kind); got != want {
			t.Errorf("kindBadge(%q) = %q, want %q", kind, got, want)
		}
	}
}
