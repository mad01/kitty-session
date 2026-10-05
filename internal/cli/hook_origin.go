package cli

import "github.com/mad01/kitty-session/internal/procinfo"

// processLookup is the slice of procinfo the nested-claude guard needs. Tests
// inject a fake process tree through it.
type processLookup struct {
	parentOf func(pid int) (int, error)
	commOf   func(pid int) (string, error)
}

// hookProcess resolves real processes. It is a variable only so hook tests,
// whose grandparent is the go tool rather than kitty, can substitute a fake.
var hookProcess = processLookup{parentOf: procinfo.ParentOf, commOf: procinfo.CommOf}

// kittyComm is the executable name the kernel records for the kitty process.
const kittyComm = "kitty"

// maxShellHops bounds the walk past shell wrappers between the hook and the
// Claude that ran it; one is the realistic depth.
const maxShellHops = 3

// shellComms are the shells Claude Code may run a hook command through, so a
// hook's parent is either Claude itself or one of these.
var shellComms = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true}

// firedByTopLevelClaude reports whether the Claude that ran this hook is the
// one ks launched. parent is the hook's parent process. The Claude ks launches
// via kitty @ launch is a direct child of kitty, while a claude started from
// inside a session (say `claude -p` from the Bash tool) sits under a shell
// under the outer Claude. The nested one inherits KS_SESSION_NAME, so only the
// process tree can tell them apart.
func firedByTopLevelClaude(lookup processLookup, parent int) (bool, error) {
	claudePID, err := skipShells(lookup, parent)
	if err != nil {
		return false, err
	}
	launcherPID, err := lookup.parentOf(claudePID)
	if err != nil {
		return false, err
	}
	comm, err := lookup.commOf(launcherPID)
	if err != nil {
		return false, err
	}
	return comm == kittyComm, nil
}

// skipShells walks up from pid past shell wrappers and returns the first
// non-shell ancestor: the Claude process that fired the hook.
func skipShells(lookup processLookup, pid int) (int, error) {
	for range maxShellHops {
		comm, err := lookup.commOf(pid)
		if err != nil {
			return 0, err
		}
		if !shellComms[comm] {
			return pid, nil
		}
		if pid, err = lookup.parentOf(pid); err != nil {
			return 0, err
		}
	}
	return pid, nil
}
