package search

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mad01/kitty-session/internal/repo/finder"
)

// setupTestRepo creates a temp directory with a known file structure and
// returns the repo path and a cleanup function.
func setupTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	files := map[string]string{
		"main.go":        "package main\nfunc Hello() string { return \"hello\" }\n",
		"util/helper.go": "package util\nfunc Add(a, b int) int { return a + b }\n",
		"README.md":      "# Test Repo\nSome documentation\n",
	}
	for rel, content := range files {
		abs := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(abs), err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", abs, err)
		}
	}
	return dir
}

// indexTestRepo indexes the test repo into a temp index directory and returns
// the index dir, the repo, and the repoNames map used by Search.
func indexTestRepo(t *testing.T, repoPath string) (indexDir string, repo finder.Repo, repoNames map[string]string) {
	t.Helper()
	indexDir = t.TempDir()
	repo = finder.Repo{Name: "test/repo", Path: repoPath}
	if err := IndexRepo(indexDir, repo); err != nil {
		t.Fatalf("IndexRepo: %v", err)
	}
	repoNames = map[string]string{repo.Name: repo.Path}
	return indexDir, repo, repoNames
}

// ---------- IndexRepo ----------

func TestIndexRepo(t *testing.T) {
	repoPath := setupTestRepo(t)
	indexDir := t.TempDir()
	repo := finder.Repo{Name: "test/repo", Path: repoPath}

	if err := IndexRepo(indexDir, repo); err != nil {
		t.Fatalf("IndexRepo returned error: %v", err)
	}

	// Verify shard files exist.
	entries, err := os.ReadDir(indexDir)
	if err != nil {
		t.Fatalf("read index dir: %v", err)
	}

	var shards int
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".zoekt") {
			shards++
		}
	}
	if shards == 0 {
		t.Fatalf("expected .zoekt shard files in %s, found none", indexDir)
	}
}

func TestIndexRepo_EmptyDir(t *testing.T) {
	repoPath := t.TempDir()
	indexDir := t.TempDir()
	repo := finder.Repo{Name: "test/empty", Path: repoPath}

	// Indexing an empty directory should not error.
	if err := IndexRepo(indexDir, repo); err != nil {
		t.Fatalf("IndexRepo on empty dir returned error: %v", err)
	}
}

// ---------- IndexRepos ----------

func TestIndexRepos(t *testing.T) {
	repoPath := setupTestRepo(t)
	indexDir := t.TempDir()

	repos := []finder.Repo{
		{Name: "test/repo", Path: repoPath},
	}

	var called int
	progress := func(i, total int, repo finder.Repo) {
		called++
	}

	if err := IndexRepos(indexDir, repos, progress); err != nil {
		t.Fatalf("IndexRepos: %v", err)
	}
	if called != 1 {
		t.Fatalf("expected progress called 1 time, got %d", called)
	}
}

func TestIndexRepos_NilProgress(t *testing.T) {
	repoPath := setupTestRepo(t)
	indexDir := t.TempDir()
	repos := []finder.Repo{{Name: "test/repo", Path: repoPath}}

	if err := IndexRepos(indexDir, repos, nil); err != nil {
		t.Fatalf("IndexRepos with nil progress: %v", err)
	}
}

// ---------- Search ----------

func TestSearch(t *testing.T) {
	repoPath := setupTestRepo(t)
	indexDir, _, repoNames := indexTestRepo(t, repoPath)
	ctx := context.Background()

	tests := []struct {
		name    string
		pattern string
		wantMin int // minimum expected match count
		checkFn func(t *testing.T, matches []Match)
	}{
		{
			name:    "simple function name",
			pattern: "Hello",
			wantMin: 1,
			checkFn: func(t *testing.T, matches []Match) {
				t.Helper()
				m := matches[0]
				if m.Repo != "test/repo" {
					t.Errorf("Repo = %q, want %q", m.Repo, "test/repo")
				}
				if m.File != "main.go" {
					t.Errorf("File = %q, want %q", m.File, "main.go")
				}
				if !strings.Contains(m.Text, "Hello") {
					t.Errorf("Text = %q, should contain %q", m.Text, "Hello")
				}
			},
		},
		{
			name:    "function signature in subdirectory",
			pattern: "func Add",
			wantMin: 1,
			checkFn: func(t *testing.T, matches []Match) {
				t.Helper()
				m := matches[0]
				if m.File != "util/helper.go" {
					t.Errorf("File = %q, want %q", m.File, "util/helper.go")
				}
				if m.Line < 1 {
					t.Errorf("Line = %d, want >= 1", m.Line)
				}
			},
		},
		{
			name:    "documentation content",
			pattern: "documentation",
			wantMin: 1,
			checkFn: func(t *testing.T, matches []Match) {
				t.Helper()
				m := matches[0]
				if m.File != "README.md" {
					t.Errorf("File = %q, want %q", m.File, "README.md")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := SearchOptions{Pattern: tc.pattern, Limit: 50}
			matches, err := Search(ctx, indexDir, opts, repoNames)
			if err != nil {
				t.Fatalf("Search(%q): %v", tc.pattern, err)
			}
			if len(matches) < tc.wantMin {
				t.Fatalf("Search(%q) returned %d matches, want >= %d", tc.pattern, len(matches), tc.wantMin)
			}
			if tc.checkFn != nil {
				tc.checkFn(t, matches)
			}
		})
	}
}

func TestSearch_WithFilters(t *testing.T) {
	repoPath := setupTestRepo(t)
	indexDir, _, repoNames := indexTestRepo(t, repoPath)
	ctx := context.Background()

	tests := []struct {
		name      string
		opts      SearchOptions
		wantMin   int
		wantMax   int // -1 means no upper bound
		checkFile string
	}{
		{
			name: "lang:go filters to Go files only",
			opts: SearchOptions{
				Pattern: "func",
				Lang:    "go",
				Limit:   50,
			},
			wantMin: 1,
			wantMax: -1,
		},
		{
			name: "file filter to helper.go",
			opts: SearchOptions{
				Pattern:    "func",
				FileFilter: "helper",
				Limit:      50,
			},
			wantMin:   1,
			wantMax:   -1,
			checkFile: "util/helper.go",
		},
		{
			name: "repo filter matches test/repo",
			opts: SearchOptions{
				Pattern:    "Hello",
				RepoFilter: "test/repo",
				Limit:      50,
			},
			wantMin: 1,
			wantMax: -1,
		},
		{
			name: "repo filter excludes nonexistent repo",
			opts: SearchOptions{
				Pattern:    "Hello",
				RepoFilter: "other/nonexistent",
				Limit:      50,
			},
			wantMin: 0,
			wantMax: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			matches, err := Search(ctx, indexDir, tc.opts, repoNames)
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			if len(matches) < tc.wantMin {
				t.Errorf("got %d matches, want >= %d", len(matches), tc.wantMin)
			}
			if tc.wantMax >= 0 && len(matches) > tc.wantMax {
				t.Errorf("got %d matches, want <= %d", len(matches), tc.wantMax)
			}
			if tc.checkFile != "" {
				for _, m := range matches {
					if m.File != tc.checkFile {
						t.Errorf("File = %q, want only %q", m.File, tc.checkFile)
					}
				}
			}
		})
	}
}

func TestSearch_NoResults(t *testing.T) {
	repoPath := setupTestRepo(t)
	indexDir, _, repoNames := indexTestRepo(t, repoPath)

	matches, err := Search(context.Background(), indexDir, SearchOptions{
		Pattern: "xyznonexistent42",
		Limit:   50,
	}, repoNames)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("expected 0 matches for nonsense pattern, got %d", len(matches))
	}
}

// ---------- Count ----------

func TestCount(t *testing.T) {
	repoPath := setupTestRepo(t)
	indexDir, _, _ := indexTestRepo(t, repoPath)
	ctx := context.Background()

	tests := []struct {
		name        string
		opts        CountOptions
		wantTotalGe int // total >= this
		checkGroups func(t *testing.T, results []CountResult)
	}{
		{
			name: "basic count",
			opts: CountOptions{
				Pattern: "func",
			},
			wantTotalGe: 2, // Hello and Add
		},
		{
			name: "group by repo",
			opts: CountOptions{
				Pattern: "func",
				GroupBy: "repo",
			},
			wantTotalGe: 2,
			checkGroups: func(t *testing.T, results []CountResult) {
				t.Helper()
				if len(results) == 0 {
					t.Fatal("expected at least 1 group")
				}
				found := false
				for _, r := range results {
					if r.Group == "test/repo" {
						found = true
						if r.Count < 2 {
							t.Errorf("group %q count = %d, want >= 2", r.Group, r.Count)
						}
					}
				}
				if !found {
					t.Error("did not find group test/repo")
				}
			},
		},
		{
			name: "group by language",
			opts: CountOptions{
				Pattern: "func",
				GroupBy: "language",
			},
			wantTotalGe: 2,
			checkGroups: func(t *testing.T, results []CountResult) {
				t.Helper()
				found := false
				for _, r := range results {
					if strings.EqualFold(r.Group, "go") {
						found = true
					}
				}
				if !found {
					t.Errorf("expected a Go language group, got: %+v", results)
				}
			},
		},
		{
			name: "count with lang filter",
			opts: CountOptions{
				Pattern: "func",
				Lang:    "go",
			},
			wantTotalGe: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			results, total, err := Count(ctx, indexDir, tc.opts)
			if err != nil {
				t.Fatalf("Count: %v", err)
			}
			if total < tc.wantTotalGe {
				t.Errorf("total = %d, want >= %d", total, tc.wantTotalGe)
			}
			if tc.checkGroups != nil {
				tc.checkGroups(t, results)
			}
		})
	}
}

// ---------- ValidateQuery ----------

func TestValidateQuery(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		wantValid bool
		wantHint  string // substring expected in Hint, empty means no check
		wantError string // substring expected in Error, empty means no check
	}{
		{
			name:      "simple valid query",
			pattern:   "Hello",
			wantValid: true,
		},
		{
			name:      "valid regex",
			pattern:   "func\\s+\\w+",
			wantValid: true,
		},
		{
			name:      "valid query with filters",
			pattern:   "func lang:go",
			wantValid: true,
		},
		{
			name:      "invalid regex unmatched paren",
			pattern:   "func (Walk",
			wantValid: false,
			wantHint:  "Escape special regex",
		},
		{
			name:      "valid boolean query",
			pattern:   "Hello OR Add",
			wantValid: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			info := ValidateQuery(tc.pattern)
			if info.Valid != tc.wantValid {
				t.Errorf("Valid = %v, want %v (Error: %q, Hint: %q)", info.Valid, tc.wantValid, info.Error, info.Hint)
			}
			if tc.wantValid && info.Parsed == "" {
				t.Error("expected non-empty Parsed for valid query")
			}
			if !tc.wantValid && info.Error == "" {
				t.Error("expected non-empty Error for invalid query")
			}
			if tc.wantHint != "" && !strings.Contains(info.Hint, tc.wantHint) {
				t.Errorf("Hint = %q, want substring %q", info.Hint, tc.wantHint)
			}
			if tc.wantError != "" && !strings.Contains(info.Error, tc.wantError) {
				t.Errorf("Error = %q, want substring %q", info.Error, tc.wantError)
			}
		})
	}
}

// ---------- Re-index ----------

func TestReindex_PicksUpChanges(t *testing.T) {
	repoPath := setupTestRepo(t)
	indexDir, repo, repoNames := indexTestRepo(t, repoPath)
	ctx := context.Background()

	// Verify the original content is searchable.
	matches, err := Search(ctx, indexDir, SearchOptions{Pattern: "Hello", Limit: 50}, repoNames)
	if err != nil {
		t.Fatalf("initial Search: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("expected matches for Hello before re-index")
	}

	// Modify main.go with new content.
	newContent := "package main\nfunc Goodbye() string { return \"goodbye\" }\n"
	if err := os.WriteFile(filepath.Join(repoPath, "main.go"), []byte(newContent), 0o644); err != nil {
		t.Fatalf("write updated main.go: %v", err)
	}

	// Re-index.
	if err := IndexRepo(indexDir, repo); err != nil {
		t.Fatalf("re-IndexRepo: %v", err)
	}

	// New content should be found.
	matches, err = Search(ctx, indexDir, SearchOptions{Pattern: "Goodbye", Limit: 50}, repoNames)
	if err != nil {
		t.Fatalf("Search after re-index: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("expected matches for Goodbye after re-index, got 0")
	}
	if !strings.Contains(matches[0].Text, "Goodbye") {
		t.Errorf("match text = %q, want to contain Goodbye", matches[0].Text)
	}
}

// ---------- buildQueryString (internal) ----------

func TestBuildQueryString(t *testing.T) {
	tests := []struct {
		name string
		opts SearchOptions
		want string
	}{
		{
			name: "pattern only",
			opts: SearchOptions{Pattern: "Hello"},
			want: "Hello",
		},
		{
			name: "all filters",
			opts: SearchOptions{
				Pattern:       "func",
				RepoFilter:    "test/repo",
				FileFilter:    "main",
				Lang:          "go",
				CaseSensitive: true,
			},
			want: "func repo:test/repo file:main lang:go case:yes",
		},
		{
			name: "partial filters",
			opts: SearchOptions{
				Pattern: "foo",
				Lang:    "python",
			},
			want: "foo lang:python",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildQueryString(tc.opts)
			if got != tc.want {
				t.Errorf("buildQueryString() = %q, want %q", got, tc.want)
			}
		})
	}
}

// ---------- Benchmarks ----------

func BenchmarkIndexRepo(b *testing.B) {
	dir := b.TempDir()
	// Create N files to index.
	for i := range 50 {
		name := filepath.Join(dir, "pkg", stringFromInt(i)+".go")
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			b.Fatal(err)
		}
		content := "package pkg\n\nfunc Func" + stringFromInt(i) + "() int { return " + stringFromInt(i) + " }\n"
		if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
			b.Fatal(err)
		}
	}

	repo := finder.Repo{Name: "bench/repo", Path: dir}

	b.ResetTimer()
	for range b.N {
		indexDir := b.TempDir()
		if err := IndexRepo(indexDir, repo); err != nil {
			b.Fatalf("IndexRepo: %v", err)
		}
	}
}

func BenchmarkSearch(b *testing.B) {
	dir := b.TempDir()
	for i := range 50 {
		name := filepath.Join(dir, "pkg", stringFromInt(i)+".go")
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			b.Fatal(err)
		}
		content := "package pkg\n\nfunc Compute" + stringFromInt(i) + "(x int) int { return x * " + stringFromInt(i) + " }\n"
		if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
			b.Fatal(err)
		}
	}

	indexDir := b.TempDir()
	repo := finder.Repo{Name: "bench/repo", Path: dir}
	if err := IndexRepo(indexDir, repo); err != nil {
		b.Fatalf("IndexRepo: %v", err)
	}
	repoNames := map[string]string{repo.Name: repo.Path}
	ctx := context.Background()

	b.ResetTimer()
	for range b.N {
		_, err := Search(ctx, indexDir, SearchOptions{Pattern: "Compute", Limit: 50}, repoNames)
		if err != nil {
			b.Fatalf("Search: %v", err)
		}
	}
}

// stringFromInt is a small helper to avoid importing strconv for benchmarks.
func stringFromInt(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	return s
}
