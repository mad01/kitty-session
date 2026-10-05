//go:build darwin

package procinfo

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// ParentOf returns the parent process ID of pid.
func ParentOf(pid int) (int, error) {
	kp, err := lookup(pid)
	if err != nil {
		return 0, err
	}
	return int(kp.Eproc.Ppid), nil
}

// CommOf returns the short executable name the kernel records for pid, for
// example "kitty" or "zsh". The kernel keeps at most 16 bytes, so long names
// are truncated.
func CommOf(pid int) (string, error) {
	kp, err := lookup(pid)
	if err != nil {
		return "", err
	}
	return unix.ByteSliceToString(kp.Proc.P_comm[:]), nil
}

// lookup reads the kern.proc.pid sysctl entry for pid. A pid that is not
// running yields an error rather than an empty record.
func lookup(pid int) (*unix.KinfoProc, error) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return nil, fmt.Errorf("procinfo: kern.proc.pid %d: %w", pid, err)
	}
	return kp, nil
}
