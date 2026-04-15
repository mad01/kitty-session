package mcpserver

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mad01/kitty-session/internal/repo/config"
	"github.com/mad01/kitty-session/internal/repo/finder"
	"github.com/mad01/kitty-session/internal/search"
)

// repoLookupInput is the typed input for the ks_repo_lookup tool.
type repoLookupInput struct {
	Name string `json:"name" jsonschema:"case-insensitive regex or substring matched against the repo name (e.g. \"tboi\", \"mad01/.*\")"`
}

// repoMatch is one entry in the ks_repo_lookup result.
type repoMatch struct {
	Name   string `json:"name" jsonschema:"org/repo name extracted from the git remote URL"`
	Path   string `json:"path" jsonschema:"absolute filesystem path to the repo root"`
	Remote string `json:"remote,omitempty" jsonschema:"full origin remote URL"`
	Host   string `json:"host,omitempty" jsonschema:"git host extracted from remote URL (e.g. github.com, git.example.com)"`
}

// repoLookupOutput is the structured output of the ks_repo_lookup tool.
type repoLookupOutput struct {
	Matches []repoMatch `json:"matches" jsonschema:"matching repos; empty if no local checkout was found"`
}

func registerRepoTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "ks_repo_lookup",
		Description: "Resolve a git repo name to its local checkout path. " +
			"Use when the user mentions a repo by name and you need its absolute path before cd-ing, reading, or grepping inside it. " +
			"Matching is case-insensitive regex / substring against the org/repo name. " +
			"Returns an empty matches array if the repo is not checked out locally — in that case, tell the user the repo is not present locally; do not guess a path under ~/code/src/... or elsewhere.",
	}, handleRepoLookup)

	mcp.AddTool(s, &mcp.Tool{
		Name: "ks_repo_info",
		Description: "Check repo health before starting work. " +
			"Returns git state (branch, dirty files, modified/untracked counts), index staleness, and an action field " +
			"indicating what to do: \"ready\" (good to go), \"commit_or_stash\" (dirty working tree), " +
			"\"pull_recommended\" (index stale >30min, likely behind remote), \"needs_reindex\" (local changes not in search index). " +
			"Use this BEFORE creating branches or making changes to a repo.",
	}, handleRepoInfo)

	mcp.AddTool(s, &mcp.Tool{
		Name: "ks_repo_pull",
		Description: "Git pull a workspace repo with safety checks. " +
			"Warns if there are uncommitted changes or detached HEAD. Uses --ff-only (no merge commits). " +
			"Set force=true to pull even with uncommitted changes. " +
			"Use before creating branches on repos that may be out of date.",
	}, handleRepoPull)

	mcp.AddTool(s, &mcp.Tool{
		Name: "ks_repo_reindex",
		Description: "Reindex a specific repo in the local zoekt search index. " +
			"Use after making significant changes to ensure ks_search results are current, " +
			"or when ks_repo_info reports needs_reindex.",
	}, handleRepoReindex)
}

func handleRepoLookup(
	_ context.Context,
	_ *mcp.CallToolRequest,
	in repoLookupInput,
) (*mcp.CallToolResult, repoLookupOutput, error) {
	if in.Name == "" {
		return nil, repoLookupOutput{}, fmt.Errorf("name is required")
	}

	re, err := regexp.Compile("(?i)" + in.Name)
	if err != nil {
		return nil, repoLookupOutput{}, fmt.Errorf("invalid name pattern %q: %w", in.Name, err)
	}

	cfg, err := config.Load()
	if err != nil {
		return nil, repoLookupOutput{}, fmt.Errorf("load ks config: %w", err)
	}

	repos, err := finder.Walk(cfg.Dirs)
	if err != nil {
		return nil, repoLookupOutput{}, fmt.Errorf("walk repos: %w", err)
	}

	matches := make([]repoMatch, 0, 4)
	for _, r := range repos {
		if re.MatchString(r.Name) {
			matches = append(matches, repoMatch{Name: r.Name, Path: r.Path, Remote: r.Remote, Host: r.Host})
		}
	}

	return nil, repoLookupOutput{Matches: matches}, nil
}

// --- ks_repo_info ---

type repoInfoInput struct {
	Name string `json:"name" jsonschema:"case-insensitive regex or substring matched against the repo name"`
}

type repoInfoMatch struct {
	Name           string `json:"name" jsonschema:"org/repo name"`
	Path           string `json:"path" jsonschema:"absolute filesystem path"`
	Remote         string `json:"remote,omitempty" jsonschema:"full origin remote URL"`
	Host           string `json:"host,omitempty" jsonschema:"git host"`
	Branch         string `json:"branch" jsonschema:"current git branch"`
	Dirty          bool   `json:"dirty" jsonschema:"working tree has uncommitted changes"`
	ModifiedFiles  int    `json:"modified_files" jsonschema:"count of modified tracked files"`
	UntrackedFiles int    `json:"untracked_files" jsonschema:"count of untracked files"`
	IndexStale     bool   `json:"index_stale" jsonschema:"zoekt index does not reflect current state"`
	IndexedAt      string `json:"indexed_at" jsonschema:"when last indexed (RFC3339 or never)"`
	Action         string `json:"action" jsonschema:"suggested action: ready, commit_or_stash, pull_recommended, needs_reindex"`
}

type repoInfoOutput struct {
	Matches []repoInfoMatch `json:"matches" jsonschema:"matching repos with health info"`
}

func handleRepoInfo(
	_ context.Context,
	_ *mcp.CallToolRequest,
	in repoInfoInput,
) (*mcp.CallToolResult, repoInfoOutput, error) {
	if in.Name == "" {
		return nil, repoInfoOutput{}, fmt.Errorf("name is required")
	}

	re, err := regexp.Compile("(?i)" + in.Name)
	if err != nil {
		return nil, repoInfoOutput{}, fmt.Errorf("invalid name pattern %q: %w", in.Name, err)
	}

	cfg, err := config.Load()
	if err != nil {
		return nil, repoInfoOutput{}, fmt.Errorf("load ks config: %w", err)
	}

	repos, err := finder.Walk(cfg.Dirs)
	if err != nil {
		return nil, repoInfoOutput{}, fmt.Errorf("walk repos: %w", err)
	}

	indexDir, err := search.DefaultIndexDir()
	if err != nil {
		return nil, repoInfoOutput{}, fmt.Errorf("index dir: %w", err)
	}

	state, err := search.LoadState(indexDir)
	if err != nil {
		return nil, repoInfoOutput{}, fmt.Errorf("load index state: %w", err)
	}

	matches := make([]repoInfoMatch, 0, 4)
	for _, r := range repos {
		if !re.MatchString(r.Name) {
			continue
		}

		m := repoInfoMatch{
			Name:   r.Name,
			Path:   r.Path,
			Remote: r.Remote,
			Host:   r.Host,
		}

		// Git state
		fp, err := search.Fingerprint(r.Path)
		if err == nil {
			m.Branch = fp.Branch
			m.Dirty = fp.Dirty
		}

		mod, untracked, err := search.DirtyInfo(r.Path)
		if err == nil {
			m.ModifiedFiles = mod
			m.UntrackedFiles = untracked
		}

		// Index state
		stored, ok := state.GetRepo(r.Path)
		if !ok {
			m.IndexStale = true
			m.IndexedAt = "never"
		} else {
			m.IndexedAt = stored.IndexedAt.Format(time.RFC3339)
			m.IndexStale = stored.Fingerprint != fp.Fingerprint
		}

		// Derive action
		m.Action = deriveAction(m)
		matches = append(matches, m)
	}

	return nil, repoInfoOutput{Matches: matches}, nil
}

func deriveAction(m repoInfoMatch) string {
	if m.Dirty {
		return "commit_or_stash"
	}
	if m.IndexedAt == "never" {
		return "needs_reindex"
	}
	if m.IndexStale {
		return "needs_reindex"
	}
	// Check if index is old (>30min) — likely behind remote
	if t, err := time.Parse(time.RFC3339, m.IndexedAt); err == nil {
		if time.Since(t) > 30*time.Minute {
			return "pull_recommended"
		}
	}
	return "ready"
}

// --- ks_repo_pull ---

type repoPullInput struct {
	Name  string `json:"name" jsonschema:"repo name (case-insensitive substring match)"`
	Force bool   `json:"force,omitempty" jsonschema:"pull even if uncommitted changes exist"`
}

type repoPullOutput struct {
	Name    string `json:"name" jsonschema:"org/repo name"`
	Path    string `json:"path" jsonschema:"absolute filesystem path"`
	Branch  string `json:"branch" jsonschema:"current branch"`
	Updated bool   `json:"updated" jsonschema:"true if HEAD changed after pull"`
	Warning string `json:"warning,omitempty" jsonschema:"warning about repo state (dirty, detached HEAD)"`
	OldHEAD string `json:"old_head,omitempty" jsonschema:"HEAD before pull"`
	NewHEAD string `json:"new_head,omitempty" jsonschema:"HEAD after pull"`
}

func handleRepoPull(
	_ context.Context,
	_ *mcp.CallToolRequest,
	in repoPullInput,
) (*mcp.CallToolResult, repoPullOutput, error) {
	if in.Name == "" {
		return nil, repoPullOutput{}, fmt.Errorf("name is required")
	}

	repo, err := resolveRepo(in.Name)
	if err != nil {
		return nil, repoPullOutput{}, err
	}

	out := repoPullOutput{
		Name: repo.Name,
		Path: repo.Path,
	}

	// Get current state
	fp, err := search.Fingerprint(repo.Path)
	if err != nil {
		return nil, repoPullOutput{}, fmt.Errorf("fingerprint %s: %w", repo.Name, err)
	}
	out.Branch = fp.Branch
	out.OldHEAD = fp.HEAD

	// Safety checks
	if fp.Branch == "HEAD" {
		out.Warning = "detached HEAD — pull may not work as expected"
		if !in.Force {
			return nil, out, nil
		}
	}
	if fp.Dirty && !in.Force {
		mod, untracked, _ := search.DirtyInfo(repo.Path)
		out.Warning = fmt.Sprintf("uncommitted changes (%d modified, %d untracked) — use force=true to pull anyway", mod, untracked)
		return nil, out, nil
	}

	// Pull
	cmd := exec.Command("git", "pull", "--ff-only")
	cmd.Dir = repo.Path
	if pullOut, err := cmd.CombinedOutput(); err != nil {
		return nil, repoPullOutput{}, fmt.Errorf("git pull in %s: %w\n%s", repo.Path, err, strings.TrimSpace(string(pullOut)))
	}

	// Check new HEAD
	fpAfter, err := search.Fingerprint(repo.Path)
	if err == nil {
		out.NewHEAD = fpAfter.HEAD
		out.Updated = out.OldHEAD != out.NewHEAD
	}

	return nil, out, nil
}

// --- ks_repo_reindex ---

type repoReindexInput struct {
	Name string `json:"name" jsonschema:"repo name (case-insensitive substring match)"`
}

type repoReindexOutput struct {
	Name      string `json:"name" jsonschema:"org/repo name"`
	Path      string `json:"path" jsonschema:"absolute filesystem path"`
	Reindexed bool   `json:"reindexed" jsonschema:"true if indexing succeeded"`
	Duration  string `json:"duration" jsonschema:"how long indexing took"`
}

func handleRepoReindex(
	_ context.Context,
	_ *mcp.CallToolRequest,
	in repoReindexInput,
) (*mcp.CallToolResult, repoReindexOutput, error) {
	if in.Name == "" {
		return nil, repoReindexOutput{}, fmt.Errorf("name is required")
	}

	repo, err := resolveRepo(in.Name)
	if err != nil {
		return nil, repoReindexOutput{}, err
	}

	indexDir, err := search.DefaultIndexDir()
	if err != nil {
		return nil, repoReindexOutput{}, fmt.Errorf("index dir: %w", err)
	}

	start := time.Now()
	if err := search.IndexRepo(indexDir, repo); err != nil {
		return nil, repoReindexOutput{}, fmt.Errorf("index %s: %w", repo.Name, err)
	}
	dur := time.Since(start)

	// Update state
	state, err := search.LoadState(indexDir)
	if err == nil {
		fp, err := search.Fingerprint(repo.Path)
		if err == nil {
			fp.IndexedAt = time.Now()
			state.SetRepo(repo.Path, fp)
			_ = state.Save(indexDir)
		}
	}

	return nil, repoReindexOutput{
		Name:      repo.Name,
		Path:      repo.Path,
		Reindexed: true,
		Duration:  dur.Truncate(time.Millisecond).String(),
	}, nil
}

// resolveRepo finds a single repo by name. Returns an error if no match or ambiguous.
func resolveRepo(name string) (finder.Repo, error) {
	re, err := regexp.Compile("(?i)" + name)
	if err != nil {
		return finder.Repo{}, fmt.Errorf("invalid name pattern %q: %w", name, err)
	}

	cfg, err := config.Load()
	if err != nil {
		return finder.Repo{}, fmt.Errorf("load ks config: %w", err)
	}

	repos, err := finder.Walk(cfg.Dirs)
	if err != nil {
		return finder.Repo{}, fmt.Errorf("walk repos: %w", err)
	}

	var matches []finder.Repo
	for _, r := range repos {
		if re.MatchString(r.Name) {
			matches = append(matches, r)
		}
	}

	if len(matches) == 0 {
		return finder.Repo{}, fmt.Errorf("no repo matching %q found locally", name)
	}
	if len(matches) > 1 {
		// Try exact match first
		for _, m := range matches {
			if strings.EqualFold(m.Name, name) {
				return m, nil
			}
		}
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = m.Name
		}
		return finder.Repo{}, fmt.Errorf("ambiguous: %d repos match %q: %s — be more specific", len(matches), name, strings.Join(names, ", "))
	}

	return matches[0], nil
}
