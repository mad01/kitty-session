package state

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// The state dir resolves HOME once per process, so every case shares one
// fake HOME under a single test function.
func TestWriteEntry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if dir := Dir(); !strings.HasPrefix(dir, home) {
		t.Fatalf("Dir() = %s, want a path under %s", dir, home)
	}

	t.Run("round trip with source", func(t *testing.T) {
		in := Entry{State: "input", AgentID: "agent-7", Tool: "mcp__linear__", Source: SourceScreen}
		if err := WriteEntry("demo", in); err != nil {
			t.Fatalf("WriteEntry: %v", err)
		}
		got, err := ReadEntry("demo")
		if err != nil {
			t.Fatalf("ReadEntry: %v", err)
		}
		if got.UpdatedAt.IsZero() || !IsFresh(got.UpdatedAt) {
			t.Errorf("UpdatedAt = %v, want a fresh stamp", got.UpdatedAt)
		}
		got.UpdatedAt = in.UpdatedAt
		if got != in {
			t.Errorf("ReadEntry = %+v, want %+v", got, in)
		}
	})

	t.Run("a bare Write has no source", func(t *testing.T) {
		if err := Write("bare", "idle"); err != nil {
			t.Fatalf("Write: %v", err)
		}
		got, err := ReadEntry("bare")
		if err != nil {
			t.Fatalf("ReadEntry: %v", err)
		}
		if got.State != "idle" || got.Source != "" || got.Tool != "" || got.AgentID != "" {
			t.Errorf("ReadEntry = %+v, want a bare idle", got)
		}
		data, err := os.ReadFile(filepath.Join(Dir(), "bare.json"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "source") {
			t.Errorf("bare entry carries a source key: %s", data)
		}
	})

	t.Run("replaces the file whole and leaves no temp file", func(t *testing.T) {
		var wg sync.WaitGroup
		for i := range 20 {
			wg.Go(func() {
				st := "working"
				if i%2 == 1 {
					st = "idle"
				}
				if err := WriteEntry("raced", Entry{State: st}); err != nil {
					t.Errorf("WriteEntry: %v", err)
				}
			})
		}
		wg.Wait()
		got, err := ReadEntry("raced")
		if err != nil {
			t.Fatalf("ReadEntry after racing writes: %v", err)
		}
		if got.State != "working" && got.State != "idle" {
			t.Errorf("state = %q, want one whole entry", got.State)
		}
		entries, err := os.ReadDir(Dir())
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".tmp") {
				t.Errorf("temp file left behind: %s", e.Name())
			}
		}
	})

	t.Run("missing file reports not-exist", func(t *testing.T) {
		if _, err := ReadEntry("nobody"); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("ReadEntry(nobody) err = %v, want not-exist", err)
		}
	})
}
