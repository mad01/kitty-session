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

// Entry is the JSON content of a state file. AgentID and Tool are set on an
// input entry only: the in-process subagent that asked (empty for the
// parent) and the tool the prompt is for (empty for a notification), so the
// hook that wrote it can tell whose completion clears it.
type Entry struct {
	State     string    `json:"state"`
	UpdatedAt time.Time `json:"updated_at"`
	AgentID   string    `json:"agent_id,omitempty"`
	Tool      string    `json:"tool,omitempty"`
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

// WriteEntry persists e for the named session, stamped with the current time.
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
	return os.WriteFile(filepath.Join(dir, name+".json"), data, 0o644)
}

// Write persists a bare state for the named session: no asker, no tool.
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
