package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

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

// HasTranscripts reports whether Claude Code has any transcript for sessions
// started in dir, which is what claude --continue needs to find one. With
// none, --continue prints "No conversation found" and exits.
func HasTranscripts(dir string) bool {
	project, err := projectDir(dir)
	if err != nil {
		return false
	}
	matches, err := filepath.Glob(filepath.Join(project, "*.jsonl"))
	return err == nil && len(matches) > 0
}
