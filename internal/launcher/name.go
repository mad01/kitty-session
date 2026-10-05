package launcher

import (
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// nonNameRunes matches every run of characters a session name may not hold.
var nonNameRunes = regexp.MustCompile(`[^a-z0-9-]+`)

// SuggestName returns the default session name for a directory: its base
// name, plus the checked-out git branch when dir is inside a repository,
// both lower-cased with runs of other characters collapsed to one hyphen
// (kitty-session-main). An empty dir has no suggestion.
func SuggestName(dir string) string {
	if dir == "" {
		return ""
	}
	name := SanitizeName(filepath.Base(dir))
	if branch := SanitizeName(gitBranch(dir)); branch != "" {
		name += "-" + branch
	}
	return name
}

// gitBranch returns the branch checked out in dir, or "" when dir is not in a
// git repository, HEAD is detached, or git is not installed. This is the
// launcher's one subprocess besides kitty.
func gitBranch(dir string) string {
	out, err := exec.Command("git", "-C", dir, "symbolic-ref", "--short", "-q", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// SanitizeName lower-cases s, turns every run of characters outside [a-z0-9-]
// into one hyphen, and trims hyphens from both ends.
func SanitizeName(s string) string {
	s = nonNameRunes.ReplaceAllString(strings.ToLower(s), "-")
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}
