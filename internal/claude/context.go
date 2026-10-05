package claude

import (
	"encoding/json"
	"fmt"
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

// encodePath converts a directory path to the name Claude Code gives its
// projects directory: every character outside [A-Za-z0-9] becomes "-", so
// /Users/u/.config/x is -Users-u--config-x. The leading dash is kept.
func encodePath(dir string) string {
	return strings.Map(func(r rune) rune {
		if ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') || ('0' <= r && r <= '9') {
			return r
		}
		return '-'
	}, dir)
}

// projectDir returns the directory under ~/.claude/projects that Claude Code
// keeps for sessions started in dir.
func projectDir(dir string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".claude", "projects", encodePath(dir)), nil
}

// TranscriptPath returns where Claude Code stores the transcript of session
// sessionID started in dir. Claude Code deletes transcripts after its cleanup
// period, so the file's presence tells whether claude --resume can still work.
func TranscriptPath(dir, sessionID string) (string, error) {
	project, err := projectDir(dir)
	if err != nil {
		return "", err
	}
	return filepath.Join(project, sessionID+".jsonl"), nil
}

// LatestPrompt returns the firstPrompt from the most recently modified
// non-sidechain session for the given working directory. Returns "" on any error.
func LatestPrompt(dir string) string {
	project, err := projectDir(dir)
	if err != nil {
		return ""
	}
	return latestPromptFromFile(filepath.Join(project, "sessions-index.json"))
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
