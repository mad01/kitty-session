package search

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mad01/kitty-session/internal/repo/finder"
)

const stateFileName = "state.json"

// RepoState tracks the indexed state of a single repository.
type RepoState struct {
	Fingerprint string    `json:"fingerprint"`
	HEAD        string    `json:"head"`
	Branch      string    `json:"branch"`
	Dirty       bool      `json:"dirty"`
	IndexedAt   time.Time `json:"indexed_at"`
}

// IndexState holds the indexed state of all repositories.
type IndexState struct {
	mu    sync.RWMutex
	Repos map[string]RepoState `json:"repos"` // key: absolute repo path
}

// LoadState reads the index state from disk.
// Returns an empty state if the file does not exist.
func LoadState(indexDir string) (*IndexState, error) {
	path := filepath.Join(indexDir, stateFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &IndexState{Repos: make(map[string]RepoState)}, nil
		}
		return nil, fmt.Errorf("read index state %s: %w", path, err)
	}

	var state IndexState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse index state %s: %w", path, err)
	}
	if state.Repos == nil {
		state.Repos = make(map[string]RepoState)
	}
	return &state, nil
}

// Save writes the index state to disk atomically.
func (s *IndexState) Save(indexDir string) error {
	s.mu.RLock()
	data, err := json.MarshalIndent(s, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return fmt.Errorf("marshal index state: %w", err)
	}

	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		return fmt.Errorf("create index dir %s: %w", indexDir, err)
	}

	path := filepath.Join(indexDir, stateFileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write index state %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename index state %s -> %s: %w", tmp, path, err)
	}
	return nil
}

// SetRepo updates the state for a single repo (thread-safe).
func (s *IndexState) SetRepo(repoPath string, state RepoState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Repos[repoPath] = state
}

// GetRepo returns the state for a single repo (thread-safe).
func (s *IndexState) GetRepo(repoPath string) (RepoState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rs, ok := s.Repos[repoPath]
	return rs, ok
}

// Fingerprint computes a staleness fingerprint for a repo.
// It combines HEAD hash, branch name, and git status output into a sha256 hash.
// Any change to committed state, branch, or working tree will produce a new fingerprint.
func Fingerprint(repoPath string) (RepoState, error) {
	head, err := gitOutput(repoPath, "rev-parse", "HEAD")
	if err != nil {
		return RepoState{}, fmt.Errorf("git rev-parse HEAD in %s: %w", repoPath, err)
	}

	branch, err := gitOutput(repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return RepoState{}, fmt.Errorf("git rev-parse --abbrev-ref HEAD in %s: %w", repoPath, err)
	}

	status, err := gitOutput(repoPath, "status", "--porcelain")
	if err != nil {
		return RepoState{}, fmt.Errorf("git status in %s: %w", repoPath, err)
	}

	dirty := status != ""

	h := sha256.New()
	fmt.Fprintf(h, "%s\n%s\n%s", head, branch, status)
	fingerprint := fmt.Sprintf("%x", h.Sum(nil))

	return RepoState{
		Fingerprint: fingerprint,
		HEAD:        head,
		Branch:      branch,
		Dirty:       dirty,
	}, nil
}

// StalenessResult holds the result of a staleness check.
type StalenessResult struct {
	Fresh []finder.Repo
	Stale []finder.Repo
	// Current maps repo path to its current fingerprint state.
	Current map[string]RepoState
}

// CheckStaleness compares current fingerprints against stored state.
func CheckStaleness(repos []finder.Repo, state *IndexState) (*StalenessResult, error) {
	result := &StalenessResult{
		Current: make(map[string]RepoState, len(repos)),
	}

	for _, repo := range repos {
		fp, err := Fingerprint(repo.Path)
		if err != nil {
			// If we can't fingerprint (e.g. not a git repo), treat as stale.
			result.Stale = append(result.Stale, repo)
			continue
		}
		result.Current[repo.Path] = fp

		stored, ok := state.GetRepo(repo.Path)
		if !ok || stored.Fingerprint != fp.Fingerprint {
			result.Stale = append(result.Stale, repo)
		} else {
			result.Fresh = append(result.Fresh, repo)
		}
	}

	return result, nil
}

// DirtyInfo returns the count of modified and untracked files.
func DirtyInfo(repoPath string) (modified, untracked int, err error) {
	status, err := gitOutput(repoPath, "status", "--porcelain")
	if err != nil {
		return 0, 0, err
	}
	for _, line := range strings.Split(status, "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "??") {
			untracked++
		} else {
			modified++
		}
	}
	return modified, untracked, nil
}

// gitOutput runs a git command in the given directory and returns trimmed stdout.
func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// DefaultIndexDir returns the default index directory path.
func DefaultIndexDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".config", "ks", "search-index"), nil
}

// IndexDirSize returns the total size of files in the index directory.
func IndexDirSize(indexDir string) (int64, error) {
	var size int64
	err := filepath.Walk(indexDir, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // best-effort
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}
