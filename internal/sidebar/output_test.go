package sidebar

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// Bubble Tea only reports the window size when the output has a descriptor,
// so the wrapper must keep looking like a terminal file.
var _ interface {
	io.ReadWriter
	Fd() uintptr
} = syncOutput{}

func TestSyncOutputWrapsEachWrite(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := syncOutput{f}
	for _, frame := range []string{"frame one", "frame two"} {
		n, err := out.Write([]byte(frame))
		if err != nil || n != len(frame) {
			t.Fatalf("Write(%q) = %d, %v; want %d, nil", frame, n, err, len(frame))
		}
	}
	got, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	want := "\x1b[?2026hframe one\x1b[?2026l\x1b[?2026hframe two\x1b[?2026l"
	if string(got) != want {
		t.Fatalf("wrote %q, want %q", got, want)
	}
}
