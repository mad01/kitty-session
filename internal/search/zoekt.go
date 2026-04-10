package search

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sourcegraph/zoekt"
	"github.com/sourcegraph/zoekt/index"
	"github.com/sourcegraph/zoekt/query"
	zoektsearch "github.com/sourcegraph/zoekt/search"

	"github.com/mad01/kitty-session/internal/repo/finder"
)

// IndexRepo indexes a single repo's working tree into the zoekt index directory.
// It walks the filesystem (not git objects) so uncommitted changes are included.
func IndexRepo(indexDir string, repo finder.Repo) error {
	opts := index.Options{
		IndexDir:     indexDir,
		DisableCTags: true,
		RepositoryDescription: zoekt.Repository{
			Name:   repo.Name,
			Source: repo.Path,
		},
	}
	opts.SetDefaults()

	builder, err := index.NewBuilder(opts)
	if err != nil {
		return fmt.Errorf("create index builder for %s: %w", repo.Name, err)
	}

	walkErr := filepath.Walk(repo.Path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // best-effort, skip unreadable files
		}

		// Skip hidden directories (including .git)
		if info.IsDir() {
			base := filepath.Base(path)
			if strings.HasPrefix(base, ".") && path != repo.Path {
				return filepath.SkipDir
			}
			// Skip common non-source directories
			switch base {
			case "node_modules", "vendor", "__pycache__", "build", "dist", "target":
				return filepath.SkipDir
			}
			return nil
		}

		// Skip non-regular files
		if !info.Mode().IsRegular() {
			return nil
		}

		// Skip very large files (>1MB by default, zoekt handles this too)
		if info.Size() > int64(opts.SizeMax) {
			return nil
		}

		relPath, err := filepath.Rel(repo.Path, path)
		if err != nil {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil // skip unreadable files
		}

		return builder.AddFile(relPath, content)
	})

	if walkErr != nil {
		_ = builder.Finish()
		return fmt.Errorf("walk %s: %w", repo.Path, walkErr)
	}

	if err := builder.Finish(); err != nil {
		return fmt.Errorf("finish index for %s: %w", repo.Name, err)
	}
	return nil
}

// IndexRepos indexes multiple repos, calling progress after each one completes.
// progress receives the current index (0-based) and total count.
func IndexRepos(indexDir string, repos []finder.Repo, progress func(i, total int, repo finder.Repo)) error {
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		return fmt.Errorf("create index dir %s: %w", indexDir, err)
	}

	for i, repo := range repos {
		if err := IndexRepo(indexDir, repo); err != nil {
			return err
		}
		if progress != nil {
			progress(i, len(repos), repo)
		}
	}
	return nil
}

// Search queries the zoekt index and returns matches.
// Opens a new searcher per call. For reusing an existing searcher, use SearchWith.
func Search(ctx context.Context, indexDir string, opts SearchOptions, repoNames map[string]string) ([]Match, error) {
	searcher, err := zoektsearch.NewDirectorySearcher(indexDir)
	if err != nil {
		return nil, fmt.Errorf("open index at %s: %w\n\nHint: run 'ks index' to build the search index", indexDir, err)
	}
	defer searcher.Close()
	return SearchWith(ctx, searcher, opts, repoNames)
}

// SearchWith queries an existing searcher and returns matches.
// Use this with a long-lived searcher (e.g. from the daemon) to avoid per-call mmap overhead.
func SearchWith(ctx context.Context, searcher zoekt.Searcher, opts SearchOptions, repoNames map[string]string) ([]Match, error) {
	qStr := buildQueryString(opts)
	q, err := query.Parse(qStr)
	if err != nil {
		return nil, fmt.Errorf("query parse error: %w\n\nHint: run 'ks query %q' to validate your query", err, opts.Pattern)
	}
	q = query.Map(q, query.ExpandFileContent)
	q = query.Simplify(q)

	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}

	sOpts := zoekt.SearchOptions{
		NumContextLines:    opts.ContextLines,
		TotalMaxMatchCount: limit * 10,
		ShardMaxMatchCount: limit * 5,
	}

	result, err := searcher.Search(ctx, q, &sOpts)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	return convertResults(result.Files, limit, repoNames), nil
}

// Count queries the zoekt index and returns match counts.
// Opens a new searcher per call. For reusing an existing searcher, use CountWith.
func Count(ctx context.Context, indexDir string, opts CountOptions) ([]CountResult, int, error) {
	searcher, err := zoektsearch.NewDirectorySearcher(indexDir)
	if err != nil {
		return nil, 0, fmt.Errorf("open index at %s: %w\n\nHint: run 'ks index' to build the search index", indexDir, err)
	}
	defer searcher.Close()
	return CountWith(ctx, searcher, opts)
}

// CountWith queries an existing searcher and returns match counts.
// Use this with a long-lived searcher (e.g. from the daemon) to avoid per-call mmap overhead.
func CountWith(ctx context.Context, searcher zoekt.Searcher, opts CountOptions) ([]CountResult, int, error) {
	qStr := opts.Pattern
	if opts.RepoFilter != "" {
		qStr += " repo:" + opts.RepoFilter
	}
	if opts.Lang != "" {
		qStr += " lang:" + opts.Lang
	}

	q, err := query.Parse(qStr)
	if err != nil {
		return nil, 0, fmt.Errorf("query parse error: %w", err)
	}
	q = query.Map(q, query.ExpandFileContent)
	q = query.Simplify(q)

	result, err := searcher.Search(ctx, q, &zoekt.SearchOptions{})
	if err != nil {
		return nil, 0, fmt.Errorf("search failed: %w", err)
	}

	total := 0
	groups := make(map[string]int)
	for _, f := range result.Files {
		count := len(f.LineMatches) + len(f.ChunkMatches)
		if count == 0 {
			count = 1
		}
		total += count

		var key string
		switch opts.GroupBy {
		case "repo":
			key = f.Repository
		case "language":
			key = f.Language
		default:
			key = "all"
		}
		groups[key] += count
	}

	results := make([]CountResult, 0, len(groups))
	for group, count := range groups {
		results = append(results, CountResult{Group: group, Count: count})
	}
	return results, total, nil
}

// ValidateQuery parses a query and returns information about it.
func ValidateQuery(pattern string) QueryInfo {
	q, err := query.Parse(pattern)
	if err != nil {
		hint := ""
		errStr := err.Error()
		if strings.Contains(errStr, "missing closing") || strings.Contains(errStr, "missing )") {
			hint = "Escape special regex characters with backslash, e.g. \"func \\(Walk\""
		} else if strings.Contains(errStr, "invalid") {
			hint = "Check your query syntax. Use 'ks search --help' for examples."
		}
		return QueryInfo{
			Valid: false,
			Error: errStr,
			Hint:  hint,
		}
	}

	q = query.Map(q, query.ExpandFileContent)
	q = query.Simplify(q)

	return QueryInfo{
		Valid:  true,
		Parsed: q.String(),
	}
}

// buildQueryString combines SearchOptions into a zoekt query string.
func buildQueryString(opts SearchOptions) string {
	parts := []string{opts.Pattern}
	if opts.RepoFilter != "" {
		parts = append(parts, "repo:"+opts.RepoFilter)
	}
	if opts.FileFilter != "" {
		parts = append(parts, "file:"+opts.FileFilter)
	}
	if opts.Lang != "" {
		parts = append(parts, "lang:"+opts.Lang)
	}
	if opts.CaseSensitive {
		parts = append(parts, "case:yes")
	}
	return strings.Join(parts, " ")
}

// convertResults transforms zoekt FileMatches into our Match type.
func convertResults(files []zoekt.FileMatch, limit int, repoNames map[string]string) []Match {
	var matches []Match
	fileCount := 0
	for _, f := range files {
		if fileCount >= limit {
			break
		}
		fileCount++

		repoPath := repoNames[f.Repository]

		if len(f.LineMatches) > 0 {
			for _, lm := range f.LineMatches {
				col := 1
				if len(lm.LineFragments) > 0 {
					col = lm.LineFragments[0].LineOffset + 1
				}
				m := Match{
					Repo:     f.Repository,
					RepoPath: repoPath,
					File:     f.FileName,
					Line:     lm.LineNumber,
					Column:   col,
					Text:     strings.TrimSuffix(string(lm.Line), "\n"),
				}
				if len(lm.Before) > 0 {
					m.Before = string(lm.Before)
				}
				if len(lm.After) > 0 {
					m.After = string(lm.After)
				}
				matches = append(matches, m)
			}
		} else if len(f.ChunkMatches) > 0 {
			for _, cm := range f.ChunkMatches {
				for _, r := range cm.Ranges {
					matches = append(matches, Match{
						Repo:     f.Repository,
						RepoPath: repoPath,
						File:     f.FileName,
						Line:     int(r.Start.LineNumber),
						Column:   int(r.Start.Column),
						Text:     strings.TrimSuffix(string(cm.Content), "\n"),
					})
				}
			}
		} else {
			matches = append(matches, Match{
				Repo:     f.Repository,
				RepoPath: repoPath,
				File:     f.FileName,
				Line:     1,
				Column:   1,
			})
		}
	}
	return matches
}

// ShardHealth describes the health of a single index shard file.
type ShardHealth struct {
	Path    string   `json:"path"`
	Size    int64    `json:"size"`
	Repos   []string `json:"repos,omitempty"`
	OK      bool     `json:"ok"`
	Error   string   `json:"error,omitempty"`
}

// ValidateShards checks all .zoekt shard files in the index directory.
// Returns health info for each shard and a list of corrupted shard paths.
func ValidateShards(indexDir string) ([]ShardHealth, []string, error) {
	entries, err := os.ReadDir(indexDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("read index dir: %w", err)
	}

	var results []ShardHealth
	var corrupted []string

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".zoekt") {
			continue
		}

		path := filepath.Join(indexDir, e.Name())
		info, _ := e.Info()
		size := int64(0)
		if info != nil {
			size = info.Size()
		}

		health := ShardHealth{Path: path, Size: size}

		f, err := os.Open(path)
		if err != nil {
			health.Error = fmt.Sprintf("cannot open: %v", err)
			results = append(results, health)
			corrupted = append(corrupted, path)
			continue
		}

		iFile, err := index.NewIndexFile(f)
		if err != nil {
			f.Close()
			health.Error = fmt.Sprintf("cannot read index file: %v", err)
			results = append(results, health)
			corrupted = append(corrupted, path)
			continue
		}

		repos, _, err := index.ReadMetadata(iFile)
		iFile.Close()
		if err != nil {
			health.Error = fmt.Sprintf("corrupted metadata: %v", err)
			results = append(results, health)
			corrupted = append(corrupted, path)
			continue
		}

		health.OK = true
		for _, r := range repos {
			health.Repos = append(health.Repos, r.Name)
		}
		results = append(results, health)
	}

	return results, corrupted, nil
}

// RepairIndex removes corrupted shard files and clears their state entries.
// Returns the list of removed shard paths.
func RepairIndex(indexDir string, state *IndexState) ([]string, error) {
	_, corrupted, err := ValidateShards(indexDir)
	if err != nil {
		return nil, err
	}

	var removed []string
	for _, path := range corrupted {
		if err := os.Remove(path); err != nil {
			return removed, fmt.Errorf("remove corrupted shard %s: %w", path, err)
		}
		removed = append(removed, path)
	}

	// Clear state for repos whose shards were removed so they get re-indexed.
	if len(removed) > 0 && state != nil {
		shards, _, _ := ValidateShards(indexDir)
		// Build set of repos still indexed
		indexedRepos := make(map[string]bool)
		for _, s := range shards {
			if s.OK {
				for _, r := range s.Repos {
					indexedRepos[r] = true
				}
			}
		}
		// Remove state for repos no longer indexed
		state.mu.Lock()
		for path := range state.Repos {
			// Check if any shard still covers this repo
			found := false
			for _, s := range shards {
				if s.OK {
					for _, r := range s.Repos {
						if r == filepath.Base(path) {
							found = true
						}
					}
				}
			}
			if !found {
				delete(state.Repos, path)
			}
		}
		state.mu.Unlock()
	}

	return removed, nil
}

