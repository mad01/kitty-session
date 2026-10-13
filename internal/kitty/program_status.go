package kitty

// ProgramState is a state of the Program Status Protocol (OSC 7501), or ""
// for a window that carries no record.
type ProgramState string

// The states the protocol defines. A program reports idle at rest, working
// while it runs, done when a result is ready that the user has not seen,
// blocked when it needs the user, error when it failed and stopped.
const (
	ProgramIdle    ProgramState = "idle"
	ProgramWorking ProgramState = "working"
	ProgramDone    ProgramState = "done"
	ProgramBlocked ProgramState = "blocked"
	ProgramError   ProgramState = "error"
)

// lsProgramStatus is one program_status element of `kitty @ ls`, limited to
// the one field ks reads.
type lsProgramStatus struct {
	State string `json:"state"`
}

// programUrgency ranks the states the way kitty picks the status it shows
// for a window: blocked outranks everything, idle nothing.
var programUrgency = map[ProgramState]int{
	ProgramIdle:    0,
	ProgramWorking: 1,
	ProgramDone:    2,
	ProgramError:   3,
	ProgramBlocked: 4,
}

// mostUrgent picks the state kitty shows for the window: the most urgent
// among its records. A record whose state ks does not know is skipped, so a
// state added by a later protocol revision never turns into another one.
// "" when no record qualifies.
func mostUrgent(records []lsProgramStatus) ProgramState {
	var best ProgramState
	rank := -1
	for _, r := range records {
		state := ProgramState(r.State)
		if urgency, known := programUrgency[state]; known && urgency > rank {
			best, rank = state, urgency
		}
	}
	return best
}
