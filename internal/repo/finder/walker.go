package finder

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const numWorkers = 32

// Walk scans the given directories for git repositories and returns a list of Repos.
// Repo names are extracted by reading .git/config directly (no subprocess).
// Uses a concurrent BFS: each directory is checked for .git via ReadDir.
// If found, the repo is recorded and no deeper descent occurs.
// Otherwise, subdirectories are queued for further scanning.
func Walk(dirs []string) ([]Repo, error) {
	work := make(chan string, 4096)
	type result struct {
		path   string
		name   string
		remote string
		host   string
	}
	results := make(chan result, 256)

	// Track in-flight work to know when to stop
	var inflight sync.WaitGroup

	// Seed initial directories before starting the closer goroutine
	for _, dir := range dirs {
		dir = expandTilde(dir)
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			continue
		}
		inflight.Add(1)
		work <- dir
	}

	// Workers: read a directory, check for .git, queue children
	var workerWg sync.WaitGroup
	for range numWorkers {
		workerWg.Add(1)
		go func() {
			defer workerWg.Done()
			for dir := range work {
				entries, err := os.ReadDir(dir)
				if err != nil {
					inflight.Done()
					continue
				}

				// Check if this directory is a git repo
				isRepo := false
				for _, e := range entries {
					if e.Name() == ".git" {
						isRepo = true
						break
					}
				}

				if isRepo {
					name, remote, host := repoInfo(dir)
					if name != "" {
						results <- result{path: dir, name: name, remote: remote, host: host}
					}
					inflight.Done()
					continue
				}

				// Not a repo — queue subdirectories for scanning
				for _, e := range entries {
					if !e.IsDir() || e.Name()[0] == '.' {
						continue
					}
					inflight.Add(1)
					work <- filepath.Join(dir, e.Name())
				}
				inflight.Done()
			}
		}()
	}

	// Close work channel when all in-flight items are done
	go func() {
		inflight.Wait()
		close(work)
	}()

	// Collect results until workers finish
	go func() {
		workerWg.Wait()
		close(results)
	}()

	seen := make(map[string]bool)
	var repos []Repo
	for r := range results {
		if !seen[r.path] {
			seen[r.path] = true
			repos = append(repos, Repo{Name: r.name, Path: r.path, Remote: r.remote, Host: r.host})
		}
	}
	return repos, nil
}

// repoInfo reads the origin remote URL from .git/config and returns
// the parsed name (org/repo), the raw remote URL, and the extracted hostname.
func repoInfo(dir string) (name, remote, host string) {
	raw := readOriginURL(filepath.Join(dir, ".git", "config"))
	if raw != "" {
		return ParseRemote(raw), raw, ParseHost(raw)
	}
	// Fallback to directory name — no remote info available
	return filepath.Base(filepath.Dir(dir)) + "/" + filepath.Base(dir), "", ""
}

// readOriginURL parses a git config file to extract the URL of [remote "origin"].
func readOriginURL(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	inOrigin := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == `[remote "origin"]` {
			inOrigin = true
			continue
		}
		if inOrigin {
			if strings.HasPrefix(line, "[") {
				return "" // next section, no url found
			}
			if strings.HasPrefix(line, "url = ") {
				return strings.TrimPrefix(line, "url = ")
			}
		}
	}
	return ""
}

func expandTilde(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return p
		}
		return filepath.Join(home, p[2:])
	}
	return p
}
