package finder

import (
	"path"
	"strings"
)

// ParseRemote extracts "org/repo" from a git remote URL.
// Supports SSH (git@host:org/repo.git) and HTTPS (https://host/org/repo.git) formats.
func ParseRemote(url string) string {
	url = strings.TrimSpace(url)
	if url == "" {
		return ""
	}

	// SSH format: git@github.com:org/repo.git
	if i := strings.Index(url, ":"); i != -1 && !strings.Contains(url, "://") {
		url = url[i+1:]
		url = strings.TrimSuffix(url, ".git")
		return url
	}

	// HTTPS format: https://github.com/org/repo.git
	// Strip scheme
	if i := strings.Index(url, "://"); i != -1 {
		url = url[i+3:]
	}

	// Remove host
	if i := strings.Index(url, "/"); i != -1 {
		url = url[i+1:]
	}

	url = strings.TrimSuffix(url, ".git")
	url = strings.TrimSuffix(url, "/")

	// Return last two path components (org/repo)
	parts := strings.Split(url, "/")
	if len(parts) >= 2 {
		return path.Join(parts[len(parts)-2], parts[len(parts)-1])
	}
	return url
}
