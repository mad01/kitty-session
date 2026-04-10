package daemon

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestDefaultSocketPath(t *testing.T) {
	p := DefaultSocketPath()
	if !strings.HasSuffix(p, filepath.Join(".config", "ks", "search-daemon.sock")) {
		t.Fatalf("unexpected socket path: %s", p)
	}
}

func TestDefaultPIDPath(t *testing.T) {
	p := DefaultPIDPath()
	if !strings.HasSuffix(p, filepath.Join(".config", "ks", "search-daemon.pid")) {
		t.Fatalf("unexpected pid path: %s", p)
	}
}

func TestDefaultLogPath(t *testing.T) {
	p := DefaultLogPath()
	if !strings.HasSuffix(p, filepath.Join(".config", "ks", "search-daemon.log")) {
		t.Fatalf("unexpected log path: %s", p)
	}
}

func TestWriteReadPID(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "test.pid")

	if err := WritePID(pidFile); err != nil {
		t.Fatalf("WritePID: %v", err)
	}

	got, err := ReadPID(pidFile)
	if err != nil {
		t.Fatalf("ReadPID: %v", err)
	}

	want := os.Getpid()
	if got != want {
		t.Fatalf("PID mismatch: got %d, want %d", got, want)
	}
}

func TestReadPID_InvalidFile(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "missing.pid")

	_, err := ReadPID(pidFile)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestReadPID_InvalidContent(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "bad.pid")
	os.WriteFile(pidFile, []byte("not-a-number"), 0644)

	_, err := ReadPID(pidFile)
	if err == nil {
		t.Fatal("expected error for non-numeric content")
	}
}

func TestIsRunning_CurrentProcess(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "test.pid")

	os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644)

	if !IsRunning(pidFile) {
		t.Fatal("expected current process to be running")
	}
}

func TestIsRunning_DeadProcess(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "test.pid")

	os.WriteFile(pidFile, []byte("99999999"), 0644)

	if IsRunning(pidFile) {
		t.Fatal("expected PID 99999999 to not be running")
	}
}

func TestIsRunning_MissingFile(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "missing.pid")

	if IsRunning(pidFile) {
		t.Fatal("expected false for missing PID file")
	}
}

func TestRemoveStale_DeadProcess(t *testing.T) {
	dir := t.TempDir()
	sockFile := filepath.Join(dir, "test.sock")
	pidFile := filepath.Join(dir, "test.pid")

	// Create stale files with a dead PID.
	os.WriteFile(sockFile, []byte("socket"), 0644)
	os.WriteFile(pidFile, []byte("99999999"), 0644)

	if err := RemoveStale(sockFile, pidFile); err != nil {
		t.Fatalf("RemoveStale: %v", err)
	}

	if _, err := os.Stat(sockFile); !os.IsNotExist(err) {
		t.Fatal("expected socket file to be removed")
	}
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Fatal("expected pid file to be removed")
	}
}

func TestRemoveStale_AliveProcess(t *testing.T) {
	dir := t.TempDir()
	sockFile := filepath.Join(dir, "test.sock")
	pidFile := filepath.Join(dir, "test.pid")

	// Create files with the current (alive) PID.
	os.WriteFile(sockFile, []byte("socket"), 0644)
	os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644)

	if err := RemoveStale(sockFile, pidFile); err != nil {
		t.Fatalf("RemoveStale: %v", err)
	}

	// Files should NOT be removed.
	if _, err := os.Stat(sockFile); err != nil {
		t.Fatal("expected socket file to still exist")
	}
	if _, err := os.Stat(pidFile); err != nil {
		t.Fatal("expected pid file to still exist")
	}
}

func TestRemoveStale_MissingFiles(t *testing.T) {
	dir := t.TempDir()
	sockFile := filepath.Join(dir, "gone.sock")
	pidFile := filepath.Join(dir, "gone.pid")

	// No files exist, no PID file means IsRunning returns false,
	// and Remove on non-existent files should not error.
	if err := RemoveStale(sockFile, pidFile); err != nil {
		t.Fatalf("RemoveStale on missing files: %v", err)
	}
}
