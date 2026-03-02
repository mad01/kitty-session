package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type sessionsIndex struct {
	Entries []entry `json:"entries"`
}

type entry struct {
	FirstPrompt string    `json:"firstPrompt"`
	Modified    time.Time `json:"modified"`
	IsSidechain bool      `json:"isSidechain"`
}

// encodePath converts an absolute directory path to the encoding Claude uses
// for its projects directory: replace / with - and trim the leading -.
func encodePath(dir string) string {
	return strings.TrimLeft(strings.ReplaceAll(dir, "/", "-"), "-")
}

// LatestPrompt returns the firstPrompt from the most recently modified
// non-sidechain session for the given working directory. Returns "" on any error.
func LatestPrompt(dir string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	indexPath := filepath.Join(home, ".claude", "projects", encodePath(dir), "sessions-index.json")
	return latestPromptFromFile(indexPath)
}

// latestPromptFromFile reads a sessions-index.json file and returns the
// firstPrompt from the most recently modified non-sidechain entry.
func latestPromptFromFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return latestPromptFromJSON(data)
}

// latestPromptFromJSON parses sessions-index JSON and returns the firstPrompt
// from the most recently modified non-sidechain entry, truncated to 60 chars.
func latestPromptFromJSON(data []byte) string {
	var idx sessionsIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return ""
	}

	var latest entry
	var found bool
	for _, e := range idx.Entries {
		if e.IsSidechain {
			continue
		}
		if !found || e.Modified.After(latest.Modified) {
			latest = e
			found = true
		}
	}
	if !found {
		return ""
	}

	prompt := latest.FirstPrompt
	if len(prompt) > 60 {
		prompt = prompt[:60] + "\u2026"
	}
	return prompt
}
