package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// freshness is the maximum age of a state file before it's considered stale.
const freshness = 10 * time.Second

// SourceScreen marks an entry the sidebar's screen check wrote from the
// window's text, as opposed to one a hook wrote from an event.
const SourceScreen = "screen"

// Entry is the JSON content of a state file. AgentID and Tool are set on an
// input entry only: the in-process subagent that asked (empty for the
// parent) and the tool the prompt is for (empty for a notification), so the
// hook that wrote it can tell whose completion clears it. Source names the
// writer when it was not a hook (SourceScreen); a hook leaves it empty, so
// the screen check can tell whether the hooks have spoken since its own
// last write.
type Entry struct {
	State     string    `json:"state"`
	UpdatedAt time.Time `json:"updated_at"`
	AgentID   string    `json:"agent_id,omitempty"`
	Tool      string    `json:"tool,omitempty"`
	Source    string    `json:"source,omitempty"`
}

var (
	dirOnce sync.Once
	dirPath string
)

// Dir returns the state file directory (~/.config/ks/state/),
// creating it on first call.
func Dir() string {
	dirOnce.Do(func() {
		home, err := os.UserHomeDir()
		if err != nil {
			return
		}
		dirPath = filepath.Join(home, ".config", "ks", "state")
		_ = os.MkdirAll(dirPath, 0o755)
	})
	return dirPath
}

// WriteEntry persists e for the named session, stamped with the current
// time. The file is replaced atomically (written beside it, then renamed),
// so a reader never sees a partial entry, and two writers racing on it (a
// hook and a leftover hook entry, or a hook and the screen check) leave one
// whole entry or the other, never a mix.
func WriteEntry(name string, e Entry) error {
	dir := Dir()
	if dir == "" {
		return fmt.Errorf("cannot determine state directory")
	}
	e.UpdatedAt = time.Now().UTC()
	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("cannot marshal state: %w", err)
	}
	return replaceFile(filepath.Join(dir, name+".json"), data)
}

// replaceFile writes data to a temporary file in path's directory and
// renames it over path, which on POSIX swaps the whole file in one step.
func replaceFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("cannot create state file: %w", err)
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(tmp.Name(), 0o644)
	}
	if werr == nil {
		werr = os.Rename(tmp.Name(), path)
	}
	if werr != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("cannot write state file: %w", werr)
	}
	return nil
}

// Write persists a bare state for the named session: no asker, no tool, no
// source.
func Write(name, state string) error {
	return WriteEntry(name, Entry{State: state})
}

// ReadEntry returns the named session's state file as written. A missing
// file reports os.ErrNotExist.
func ReadEntry(name string) (Entry, error) {
	dir := Dir()
	if dir == "" {
		return Entry{}, fmt.Errorf("cannot determine state directory")
	}
	data, err := os.ReadFile(filepath.Join(dir, name+".json"))
	if err != nil {
		return Entry{}, err
	}
	var e Entry
	if err := json.Unmarshal(data, &e); err != nil {
		return Entry{}, fmt.Errorf("cannot parse state file: %w", err)
	}
	return e, nil
}

// Read returns the state string and timestamp for the named session.
func Read(name string) (string, time.Time, error) {
	e, err := ReadEntry(name)
	if err != nil {
		return "", time.Time{}, err
	}
	return e.State, e.UpdatedAt, nil
}

// IsFresh returns true if t is within the freshness window of now.
func IsFresh(t time.Time) bool {
	return time.Since(t) <= freshness
}

// Rename moves the state file from oldName to newName.
func Rename(oldName, newName string) {
	dir := Dir()
	if dir == "" {
		return
	}
	_ = os.Rename(filepath.Join(dir, oldName+".json"), filepath.Join(dir, newName+".json"))
}

// Clean removes the state file for the named session.
func Clean(name string) {
	dir := Dir()
	if dir == "" {
		return
	}
	os.Remove(filepath.Join(dir, name+".json"))
}
