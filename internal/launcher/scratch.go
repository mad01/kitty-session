package launcher

import (
	"fmt"
	"os"
)

// scratchPrefix starts the name of every scratch directory ks creates.
const scratchPrefix = "ks-*"

// ScratchDir creates a fresh directory for a scratch session under base,
// creating base first when it does not exist. An empty base means the OS
// temp directory.
func ScratchDir(base string) (string, error) {
	if base != "" {
		if err := os.MkdirAll(base, 0o755); err != nil {
			return "", fmt.Errorf("cannot create tmpdir: %w", err)
		}
	}
	dir, err := os.MkdirTemp(base, scratchPrefix)
	if err != nil {
		return "", fmt.Errorf("cannot create temp directory: %w", err)
	}
	return dir, nil
}
