package launcher

import (
	"time"

	"github.com/mad01/kitty-session/internal/kitty"
	"github.com/mad01/kitty-session/internal/sidebar"
	"github.com/mad01/kitty-session/internal/state"
)

// ReportedState is the state a session takes from its agent window's Program
// Status Protocol record (OSC 7501), when the record is the one to listen
// to: the window carries one, and no fresh state file (state.IsFresh)
// outranks it. A fresh file is at most the freshness threshold old and comes
// from a hook that saw the event, while a record stays until kitty drops
// it, so the file wins while it is fresh. The sidebar and ks list both
// decide this here. ok is false when the caller should fall back to the
// kind's own rules. A done the caller is looking at is turned to idle by the
// caller's own visible-tab pass, not here.
func ReportedState(report kitty.ProgramState, fileAt time.Time) (sidebar.State, bool) {
	if report == "" || state.IsFresh(fileAt) {
		return sidebar.StateIdle, false
	}
	return ProgramState(report)
}

// ProgramState maps a Program Status Protocol state onto the sidebar's:
// blocked is input, and working, done, error and idle keep their names. The
// record is the program's own word, so it reads the same whatever the
// session's kind. A done on the tab the user is looking at becomes idle in
// the single visible-tab pass the callers apply (resolveState, ks list), not
// here. ok is false without a record or with a state ks does not map, and
// the caller falls back to its own rules; the kitty package already drops
// states it does not know, so that fallthrough is defensive.
func ProgramState(st kitty.ProgramState) (sidebar.State, bool) {
	switch st {
	case kitty.ProgramBlocked:
		return sidebar.StateInput, true
	case kitty.ProgramWorking:
		return sidebar.StateWorking, true
	case kitty.ProgramDone:
		return sidebar.StateDone, true
	case kitty.ProgramError:
		return sidebar.StateError, true
	case kitty.ProgramIdle:
		return sidebar.StateIdle, true
	default:
		return sidebar.StateIdle, false
	}
}
