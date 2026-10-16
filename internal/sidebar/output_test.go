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
} = (*SyncOutput)(nil)

func TestSyncOutputWrapsEachWrite(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := NewSyncOutput(f)
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

// TestSyncOutputEmitIsNotAFrame checks that emit lands between frames as
// written, with no update markers of its own.
func TestSyncOutputEmitIsNotAFrame(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := NewSyncOutput(f)
	if _, err := out.Write([]byte("frame")); err != nil {
		t.Fatal(err)
	}
	if err := out.emit([]byte("\x1b]99;i=x:p=close;\x1b\\")); err != nil {
		t.Fatalf("emit: %v", err)
	}
	got, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	want := "\x1b[?2026hframe\x1b[?2026l\x1b]99;i=x:p=close;\x1b\\"
	if string(got) != want {
		t.Fatalf("wrote %q, want %q", got, want)
	}
}

// TestSyncOutputWriteStringIsAFrame checks that a string write, which is
// how Bubble Tea's renderer writes, carries the markers like Write does
// instead of reaching the embedded file directly.
func TestSyncOutputWriteStringIsAFrame(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := NewSyncOutput(f)
	if n, err := io.WriteString(out, "frame"); err != nil || n != 5 {
		t.Fatalf("WriteString = %d, %v; want 5, nil", n, err)
	}
	got, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if want := "\x1b[?2026hframe\x1b[?2026l"; string(got) != want {
		t.Fatalf("wrote %q, want %q", got, want)
	}
}
