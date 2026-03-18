package tui

import (
	"testing"
)

func TestClaudeLaunchArgs(t *testing.T) {
	tests := []struct {
		name       string
		sessName   string
		continuing bool
		wantLast   string // last argument
		wantNoCont bool   // true means --continue must NOT appear
	}{
		{
			name:       "new session omits --continue",
			sessName:   "my-session",
			continuing: false,
			wantLast:   "claude",
			wantNoCont: true,
		},
		{
			name:       "reopened session includes --continue",
			sessName:   "my-session",
			continuing: true,
			wantLast:   "--continue",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := claudeLaunchArgs(tt.sessName, tt.continuing)

			last := args[len(args)-1]
			if last != tt.wantLast {
				t.Errorf("last arg = %q, want %q", last, tt.wantLast)
			}

			if tt.wantNoCont {
				for _, a := range args {
					if a == "--continue" {
						t.Error("unexpected --continue in args for new session")
					}
				}
			}

			// Verify common args are always present
			hasEnvName := false
			for i, a := range args {
				if a == "--env" && i+1 < len(args) {
					if len(args[i+1]) > len("KS_SESSION_NAME=") &&
						args[i+1][:len("KS_SESSION_NAME=")] == "KS_SESSION_NAME=" {
						hasEnvName = true
					}
				}
			}
			if !hasEnvName {
				t.Error("missing KS_SESSION_NAME env arg")
			}
		})
	}
}
