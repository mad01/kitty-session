package cli

import (
	"bytes"
	"slices"
	"testing"

	"github.com/mad01/kitty-session/internal/kitty"
	"github.com/spf13/cobra"
)

func TestHomeVars(t *testing.T) {
	tests := []struct {
		name  string
		home  bool
		agent bool
		want  []string
	}{
		{name: "the home sidebar tags its window", home: true, want: []string{"KS_HOME=1"}},
		{
			name: "with the agent running it tags both", home: true, agent: true,
			want: []string{"KS_HOME=1", "KS_HOME_AGENT=1"},
		},
		{name: "a session sidebar tags nothing"},
		{name: "a session sidebar running the agent tags nothing", agent: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := homeVars(tc.home, tc.agent); !slices.Equal(got, tc.want) {
				t.Errorf("homeVars(%v, %v) = %q, want %q", tc.home, tc.agent, got, tc.want)
			}
		})
	}
}

// A sidebar run by hand in another kitty must not tag anything: its
// KITTY_WINDOW_ID is that kitty's, and nothing answers on the socket here,
// so a kitty call would have produced a warning.
func TestMarkHomeOutsideTheInstanceDoesNothing(t *testing.T) {
	t.Setenv(listenOnEnv, "unix:/tmp/another-kitty.sock")
	t.Setenv(windowIDEnv, "3")
	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetErr(&buf)
	markHome(cmd, kitty.New("unix:/tmp/ks-test.sock"), homeVars(true, false))
	if buf.Len() != 0 {
		t.Errorf("markHome reached kitty from outside the instance: %s", buf.String())
	}
}
