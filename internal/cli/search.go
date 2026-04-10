package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/alpkeskin/gotoon"
	"github.com/spf13/cobra"

	"github.com/mad01/kitty-session/internal/daemon"
	"github.com/mad01/kitty-session/internal/repo/config"
	"github.com/mad01/kitty-session/internal/repo/finder"
	"github.com/mad01/kitty-session/internal/search"
)

var (
	searchJSONFlag          bool
	searchToonFlag          bool
	searchRepoFlag          string
	searchLangFlag          string
	searchFileFlag          string
	searchLimitFlag         int
	searchOutputModeFlag    string
	searchContextLinesFlag  int
	searchCaseSensitiveFlag bool
	searchReindexFlag       bool
	searchServeFlag         bool
	searchStopFlag          bool
)

var searchCmd = &cobra.Command{
	Use:   "search <pattern>",
	Short: "Search code across all indexed repositories",
	Long: `Search code across all indexed repositories using zoekt query syntax.

Query syntax:
  literal         Exact substring match (case-insensitive by default)
  foo.*bar        Regular expression
  "exact phrase"  Quoted exact match
  foo bar         AND — both terms must appear
  foo|bar         OR — either term matches
  -test           NOT — exclude matches
  repo:name       Restrict to repos matching regex
  file:\.go$      Restrict to files matching regex
  lang:go         Restrict to a specific language
  case:yes        Force case-sensitive search

Examples:
  ks search "func Walk"
  ks search "TODO|FIXME" --repo kitty-session
  ks search "fmt\.Errorf" --lang go --output-mode content -C 3
  ks search "import.*cobra" --file "\.go$" --limit 10
  ks search --reindex "NewBuilder"

Indexing behavior:
  On first run, all configured repos are indexed (this may take a moment).
  Subsequent searches check for staleness — repos with new commits or
  dirty working trees are re-indexed in the background after results are
  returned. Use --reindex to force a synchronous re-index before searching.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runSearch,
}

func init() {
	searchCmd.Flags().BoolVar(&searchJSONFlag, "json", false, "output as JSON")
	searchCmd.Flags().BoolVar(&searchToonFlag, "toon", false, "output as TOON for LLMs")
	searchCmd.Flags().StringVarP(&searchRepoFlag, "repo", "r", "", "filter results to repos matching this name")
	searchCmd.Flags().StringVarP(&searchLangFlag, "lang", "l", "", "filter results to a specific language")
	searchCmd.Flags().StringVarP(&searchFileFlag, "file", "f", "", "filter results to files matching this regex")
	searchCmd.Flags().IntVar(&searchLimitFlag, "limit", 50, "maximum number of file results")
	searchCmd.Flags().StringVarP(&searchOutputModeFlag, "output-mode", "o", "files_with_matches", "output mode: files_with_matches or content")
	searchCmd.Flags().IntVarP(&searchContextLinesFlag, "context-lines", "C", 0, "number of context lines around each match")
	searchCmd.Flags().BoolVar(&searchCaseSensitiveFlag, "case-sensitive", false, "force case-sensitive matching")
	searchCmd.Flags().BoolVar(&searchReindexFlag, "reindex", false, "force synchronous re-index before searching")
	searchCmd.Flags().BoolVar(&searchServeFlag, "serve", false, "start the search daemon (foreground)")
	searchCmd.Flags().BoolVar(&searchStopFlag, "stop", false, "stop the running search daemon")
	rootCmd.AddCommand(searchCmd)
}

func runSearch(cmd *cobra.Command, args []string) error {
	indexDir, err := search.DefaultIndexDir()
	if err != nil {
		return err
	}
	socketPath := daemon.DefaultSocketPath()

	// Handle --serve: start daemon in foreground.
	if searchServeFlag {
		if daemon.IsRunning(daemon.DefaultPIDPath()) {
			fmt.Fprintln(cmd.ErrOrStderr(), "search daemon is already running")
			return nil
		}
		return daemon.Serve(indexDir, socketPath, 10*time.Minute)
	}

	// Handle --stop: kill running daemon.
	if searchStopFlag {
		if err := daemon.Shutdown(socketPath); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "daemon not running or already stopped: %v\n", err)
		} else {
			fmt.Fprintln(cmd.ErrOrStderr(), "search daemon stopped")
		}
		return nil
	}

	// Normal search: require pattern.
	if len(args) == 0 {
		return fmt.Errorf("pattern is required\n\nUsage: ks search <pattern>\nRun 'ks search --help' for examples")
	}
	pattern := args[0]

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w\n\nHint: create ~/.config/ks/config.yaml with a 'dirs' list", err)
	}

	repos, err := finder.Walk(cfg.Dirs)
	if err != nil {
		return err
	}

	if len(repos) == 0 {
		return fmt.Errorf("no repos found in configured directories\n\nHint: add directories to 'dirs' in ~/.config/ks/config.yaml")
	}

	// Build repo name → path map for search.
	repoNames := make(map[string]string, len(repos))
	for _, r := range repos {
		repoNames[r.Name] = r.Path
	}

	opts := search.SearchOptions{
		Pattern:       pattern,
		RepoFilter:    searchRepoFlag,
		FileFilter:    searchFileFlag,
		Lang:          searchLangFlag,
		CaseSensitive: searchCaseSensitiveFlag,
		Limit:         searchLimitFlag,
		ContextLines:  searchContextLinesFlag,
		OutputMode:    searchOutputModeFlag,
	}

	// Try daemon first (auto-starts if not running).
	if !searchReindexFlag {
		if err := daemon.EnsureDaemon(indexDir, socketPath); err == nil {
			matches, err := daemon.SearchVia(context.Background(), socketPath, opts, repoNames)
			if err == nil {
				return outputSearchResults(cmd, matches)
			}
			// Daemon failed — fall through to in-process search.
			fmt.Fprintf(cmd.ErrOrStderr(), "daemon search failed, falling back to in-process: %v\n", err)
		}
	}

	// Fallback: in-process search (handles indexing too).
	// Filter by --repo if specified.
	if searchRepoFlag != "" {
		var filtered []finder.Repo
		for _, r := range repos {
			if strings.Contains(r.Name, searchRepoFlag) {
				filtered = append(filtered, r)
			}
		}
		if len(filtered) == 0 {
			return fmt.Errorf("no repos matching %q found", searchRepoFlag)
		}
		repos = filtered
	}

	state, err := search.LoadState(indexDir)
	if err != nil {
		return err
	}

	staleness, err := search.CheckStaleness(repos, state)
	if err != nil {
		return err
	}

	// If no index exists at all, do a full sync index.
	noIndex := len(state.Repos) == 0
	if noIndex || searchReindexFlag {
		toIndex := repos
		if !noIndex && !searchReindexFlag {
			toIndex = staleness.Stale
		}
		if len(toIndex) > 0 {
			w := cmd.ErrOrStderr()
			fmt.Fprintf(w, "Indexing %d repo(s)...\n", len(toIndex))
			err := search.IndexRepos(indexDir, toIndex, func(i, total int, repo finder.Repo) {
				fmt.Fprintf(w, "  [%d/%d] %s\n", i+1, total, repo.Name)
			})
			if err != nil {
				return fmt.Errorf("indexing failed: %w", err)
			}
			for _, repo := range toIndex {
				if fp, ok := staleness.Current[repo.Path]; ok {
					fp.IndexedAt = time.Now()
					state.SetRepo(repo.Path, fp)
				}
			}
			if err := state.Save(indexDir); err != nil {
				fmt.Fprintf(w, "warning: failed to save index state: %v\n", err)
			}
		}
	}

	matches, err := search.Search(context.Background(), indexDir, opts, repoNames)
	if err != nil {
		return err
	}

	// Background re-index for stale repos.
	if !noIndex && !searchReindexFlag && len(staleness.Stale) > 0 {
		go func() {
			_ = search.IndexRepos(indexDir, staleness.Stale, nil)
			for _, repo := range staleness.Stale {
				if fp, ok := staleness.Current[repo.Path]; ok {
					state.SetRepo(repo.Path, fp)
				}
			}
			_ = state.Save(indexDir)
		}()
	}

	return outputSearchResults(cmd, matches)
}

func outputSearchResults(cmd *cobra.Command, matches []search.Match) error {
	w := cmd.OutOrStdout()

	if searchJSONFlag {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(matches)
	}

	if searchToonFlag {
		items := make([]map[string]any, len(matches))
		for i, m := range matches {
			items[i] = map[string]any{
				"repo":   m.Repo,
				"file":   m.File,
				"line":   m.Line,
				"column": m.Column,
				"text":   m.Text,
			}
		}
		encoded, err := gotoon.Encode(map[string]any{"matches": items})
		if err != nil {
			return err
		}
		fmt.Fprintln(w, encoded)
		return nil
	}

	if len(matches) == 0 {
		fmt.Fprintln(w, "no matches")
		return nil
	}

	switch searchOutputModeFlag {
	case "content":
		for _, m := range matches {
			if m.Before != "" {
				fmt.Fprint(w, m.Before)
			}
			fmt.Fprintf(w, "%s/%s:%d:%d: %s\n", m.Repo, m.File, m.Line, m.Column, m.Text)
			if m.After != "" {
				fmt.Fprint(w, m.After)
			}
		}
	default: // files_with_matches
		seen := make(map[string]bool)
		for _, m := range matches {
			key := m.Repo + "/" + m.File
			if !seen[key] {
				seen[key] = true
				fmt.Fprintf(w, "%s/%s\n", m.Repo, m.File)
			}
		}
	}

	return nil
}
