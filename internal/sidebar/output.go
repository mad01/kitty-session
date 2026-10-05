package sidebar

import (
	"os"

	"github.com/charmbracelet/x/ansi"
)

// syncOutput wraps every write to the terminal in a synchronized update
// (DEC private mode 2026), so kitty paints each Bubble Tea frame in one go.
// Recolouring the frame rewrites every line, and the macOS pty hands kitty
// that burst in small chunks; without the markers kitty can show a
// half-painted frame between two chunks, which reads as a flicker. The
// embedded file keeps Fd and Read, which Bubble Tea needs to size the window
// and restore the terminal on exit.
type syncOutput struct {
	*os.File
}

// Write sends p as one synchronized update.
func (o syncOutput) Write(p []byte) (int, error) {
	buf := make([]byte, 0, len(ansi.SetModeSynchronizedOutput)+len(p)+
		len(ansi.ResetModeSynchronizedOutput))
	buf = append(buf, ansi.SetModeSynchronizedOutput...)
	buf = append(buf, p...)
	buf = append(buf, ansi.ResetModeSynchronizedOutput...)
	if _, err := o.File.Write(buf); err != nil {
		return 0, err
	}
	return len(p), nil
}
