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

// Entry represents the JSON content of a state file.
type Entry struct {
	State     string    `json:"state"`
	UpdatedAt time.Time `json:"updated_at"`
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
		_ = os.MkdirAll(dirPath, 0755)
	})
	return dirPath
}

// Write persists state for the named session with the current timestamp.
func Write(name, state string) error {
	dir := Dir()
	if dir == "" {
		return fmt.Errorf("cannot determine state directory")
	}
	e := Entry{
		State:     state,
		UpdatedAt: time.Now().UTC(),
	}
	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("cannot marshal state: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, name+".json"), data, 0644)
}

// Read returns the state string and timestamp for the named session.
func Read(name string) (string, time.Time, error) {
	dir := Dir()
	if dir == "" {
		return "", time.Time{}, fmt.Errorf("cannot determine state directory")
	}
	data, err := os.ReadFile(filepath.Join(dir, name+".json"))
	if err != nil {
		return "", time.Time{}, err
	}
	var e Entry
	if err := json.Unmarshal(data, &e); err != nil {
		return "", time.Time{}, fmt.Errorf("cannot parse state file: %w", err)
	}
	return e.State, e.UpdatedAt, nil
}

// IsFresh returns true if t is within the freshness window of now.
func IsFresh(t time.Time) bool {
	return time.Since(t) <= freshness
}

// IsRecentlyWorking returns true if t is within a longer window (5 min),
// used to trust "working" state across gaps between tool calls.
func IsRecentlyWorking(t time.Time) bool {
	return time.Since(t) <= 5*time.Minute
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
	_ = os.Remove(filepath.Join(dir, name+".json"))
}
