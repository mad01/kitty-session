//go:build !darwin

package procinfo

// ParentOf is not implemented on this platform.
func ParentOf(int) (int, error) { return 0, ErrUnsupported }

// CommOf is not implemented on this platform.
func CommOf(int) (string, error) { return "", ErrUnsupported }
