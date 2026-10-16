package cli

import (
	"testing"

	"github.com/mad01/kitty-session/internal/repo/config"
	"github.com/mad01/kitty-session/internal/sidebar"
)

// TestNotifyLabelsMatchSidebarStates pins the contract between the config,
// which names the states a sidebar notifies on, and the sidebar, which
// matches those names against its own state labels; the two packages never
// import each other, so this is where the two sets meet.
func TestNotifyLabelsMatchSidebarStates(t *testing.T) {
	tests := []struct {
		label string
		state sidebar.State
	}{
		{config.NotifyInput, sidebar.StateInput},
		{config.NotifyDone, sidebar.StateDone},
	}
	for _, tc := range tests {
		if got := tc.state.String(); got != tc.label {
			t.Errorf("config label %q, sidebar label %q", tc.label, got)
		}
	}
}
