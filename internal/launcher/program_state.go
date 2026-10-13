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
// kind's own rules.
func ReportedState(
	report kitty.ProgramState, fileAt time.Time, tabActive bool,
) (sidebar.State, bool) {
	if report == "" || state.IsFresh(fileAt) {
		return sidebar.StateIdle, false
	}
	return ProgramState(report, tabActive)
}

// ProgramState maps a Program Status Protocol state onto the sidebar's:
// blocked is input, and working, done, error and idle keep their names,
// except that done on the tab the user is looking at reads idle. kitty
// drops a done record on a focus-in or a keypress only, so a turn that ends
// while the user sits in the window would otherwise stay at done. The
// record is the program's own word, so it reads the same whatever the
// session's kind. ok is false without a record or with a state ks does not
// map, and the caller falls back to its own rules; the kitty package
// already drops states it does not know, so that fallthrough is defensive.
func ProgramState(st kitty.ProgramState, tabActive bool) (sidebar.State, bool) {
	switch st {
	case kitty.ProgramBlocked:
		return sidebar.StateInput, true
	case kitty.ProgramWorking:
		return sidebar.StateWorking, true
	case kitty.ProgramDone:
		if tabActive {
			return sidebar.StateIdle, true
		}
		return sidebar.StateDone, true
	case kitty.ProgramError:
		return sidebar.StateError, true
	case kitty.ProgramIdle:
		return sidebar.StateIdle, true
	default:
		return sidebar.StateIdle, false
	}
}
