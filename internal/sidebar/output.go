package sidebar

import (
	"os"
	"sync"

	"github.com/charmbracelet/x/ansi"
)

// SyncOutput wraps every write to the terminal in a synchronized update
// (DEC private mode 2026), so kitty paints each Bubble Tea frame in one go.
// Recolouring the frame rewrites every line, and the macOS pty hands kitty
// that burst in small chunks; without the markers kitty can show a
// half-painted frame between two chunks, which reads as a flicker. The
// embedded file keeps Fd and Read, which Bubble Tea needs to size the window
// and restore the terminal on exit.
//
// The notifier writes to the same terminal from its own goroutine; the lock
// keeps its escape sequence out of the middle of a frame.
type SyncOutput struct {
	*os.File
	mu sync.Mutex
}

// NewSyncOutput returns the writer for the terminal f, normally stdout.
func NewSyncOutput(f *os.File) *SyncOutput {
	return &SyncOutput{File: f}
}

// Write sends p as one synchronized update.
func (o *SyncOutput) Write(p []byte) (int, error) {
	buf := make([]byte, 0, len(ansi.SetModeSynchronizedOutput)+len(p)+
		len(ansi.ResetModeSynchronizedOutput))
	buf = append(buf, ansi.SetModeSynchronizedOutput...)
	buf = append(buf, p...)
	buf = append(buf, ansi.ResetModeSynchronizedOutput...)
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, err := o.File.Write(buf); err != nil {
		return 0, err
	}
	return len(p), nil
}

// WriteString is Write for a string. Bubble Tea's renderer writes through
// io.WriteString, which would otherwise reach the embedded file's own
// WriteString and skip both the lock and the markers.
func (o *SyncOutput) WriteString(s string) (int, error) {
	return o.Write([]byte(s))
}

// emit sends p as one write between frames, without update markers: it is
// for escape sequences the terminal acts on, not for anything it paints.
func (o *SyncOutput) emit(p []byte) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	_, err := o.File.Write(p)
	return err
}
