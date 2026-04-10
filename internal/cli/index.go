package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/mad01/kitty-session/internal/repo/config"
	"github.com/mad01/kitty-session/internal/repo/finder"
	"github.com/mad01/kitty-session/internal/search"
)

var (
	indexAllFlag    bool
	indexStatusFlag bool
	indexCleanFlag  bool
	indexRepairFlag bool
	indexJSONFlag   bool
)

var indexCmd = &cobra.Command{
	Use:   "index",
	Short: "Manage the search index",
	Long: `Manage the search index for code search.

By default, only stale repos (new commits, dirty working tree) are re-indexed.
Use --all to force a full re-index of every repo.
Use --status to view the current index state.
Use --repair to validate and fix corrupted shard files.
Use --clean to delete the entire index directory.`,
	RunE: runIndex,
}

func init() {
	indexCmd.Flags().BoolVar(&indexAllFlag, "all", false, "force full re-index of all repos")
	indexCmd.Flags().BoolVar(&indexStatusFlag, "status", false, "show index status table")
	indexCmd.Flags().BoolVar(&indexCleanFlag, "clean", false, "delete the index directory")
	indexCmd.Flags().BoolVar(&indexRepairFlag, "repair", false, "validate shards and remove corrupted ones")
	indexCmd.Flags().BoolVar(&indexJSONFlag, "json", false, "output as JSON")
	rootCmd.AddCommand(indexCmd)
}

type indexStatusJSON struct {
	Repo        string `json:"repo"`
	Path        string `json:"path"`
	Status      string `json:"status"`
	HEAD        string `json:"head,omitempty"`
	Branch      string `json:"branch,omitempty"`
	Dirty       bool   `json:"dirty"`
	IndexedAt   string `json:"indexed_at,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

func runIndex(cmd *cobra.Command, args []string) error {
	indexDir, err := search.DefaultIndexDir()
	if err != nil {
		return err
	}

	if indexCleanFlag {
		if err := os.RemoveAll(indexDir); err != nil {
			return fmt.Errorf("failed to remove index directory %s: %w", indexDir, err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "removed %s\n", indexDir)
		return nil
	}

	if indexRepairFlag {
		return runRepair(cmd, indexDir)
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	repos, err := finder.Walk(cfg.Dirs)
	if err != nil {
		return err
	}

	state, err := search.LoadState(indexDir)
	if err != nil {
		return err
	}

	staleness, err := search.CheckStaleness(repos, state)
	if err != nil {
		return err
	}

	if indexStatusFlag {
		return printIndexStatus(cmd, repos, state, staleness)
	}

	// Determine what to index.
	toIndex := staleness.Stale
	if indexAllFlag {
		toIndex = repos
	}

	if len(toIndex) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "all repos are up to date")
		return nil
	}

	w := cmd.ErrOrStderr()
	fmt.Fprintf(w, "Indexing %d repo(s)...\n", len(toIndex))

	err = search.IndexRepos(indexDir, toIndex, func(i, total int, repo finder.Repo) {
		fmt.Fprintf(w, "  [%d/%d] %s\n", i+1, total, repo.Name)
	})
	if err != nil {
		return fmt.Errorf("indexing failed: %w", err)
	}

	// Update state.
	for _, repo := range toIndex {
		if fp, ok := staleness.Current[repo.Path]; ok {
			fp.IndexedAt = time.Now()
			state.SetRepo(repo.Path, fp)
		}
	}
	if err := state.Save(indexDir); err != nil {
		return fmt.Errorf("failed to save index state: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "indexed %d repo(s)\n", len(toIndex))
	return nil
}

func printIndexStatus(cmd *cobra.Command, repos []finder.Repo, state *search.IndexState, staleness *search.StalenessResult) error {
	w := cmd.OutOrStdout()

	staleSet := make(map[string]bool)
	for _, r := range staleness.Stale {
		staleSet[r.Path] = true
	}

	if indexJSONFlag {
		items := make([]indexStatusJSON, 0, len(repos))
		for _, r := range repos {
			status := "fresh"
			if staleSet[r.Path] {
				status = "stale"
			}
			rs, ok := state.GetRepo(r.Path)
			item := indexStatusJSON{
				Repo:   r.Name,
				Path:   r.Path,
				Status: status,
			}
			if ok {
				item.HEAD = rs.HEAD
				item.Branch = rs.Branch
				item.Dirty = rs.Dirty
				item.IndexedAt = rs.IndexedAt.Format(time.RFC3339)
				item.Fingerprint = rs.Fingerprint
			} else {
				item.Status = "missing"
			}
			items = append(items, item)
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}

	fmt.Fprintf(w, "%-40s %-10s %-8s %-20s %s\n", "REPO", "STATUS", "DIRTY", "INDEXED AT", "BRANCH")
	for _, r := range repos {
		status := "fresh"
		if staleSet[r.Path] {
			status = "stale"
		}
		rs, ok := state.GetRepo(r.Path)
		if !ok {
			fmt.Fprintf(w, "%-40s %-10s %-8s %-20s %s\n", r.Name, "missing", "-", "-", "-")
			continue
		}
		dirty := "no"
		if rs.Dirty {
			dirty = "yes"
		}
		indexed := "-"
		if !rs.IndexedAt.IsZero() {
			indexed = time.Since(rs.IndexedAt).Truncate(time.Second).String() + " ago"
		}
		fmt.Fprintf(w, "%-40s %-10s %-8s %-20s %s\n", r.Name, status, dirty, indexed, rs.Branch)
	}
	return nil
}

func runRepair(cmd *cobra.Command, indexDir string) error {
	w := cmd.OutOrStdout()

	shards, corrupted, err := search.ValidateShards(indexDir)
	if err != nil {
		return fmt.Errorf("validate shards: %w", err)
	}

	if indexJSONFlag {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(shards)
	}

	fmt.Fprintf(w, "Validating %d shard(s)...\n", len(shards))

	healthy := 0
	for _, s := range shards {
		if s.OK {
			healthy++
		} else {
			fmt.Fprintf(w, "  CORRUPT: %s — %s\n", filepath.Base(s.Path), s.Error)
		}
	}
	fmt.Fprintf(w, "  %d healthy, %d corrupted\n", healthy, len(corrupted))

	if len(corrupted) == 0 {
		fmt.Fprintln(w, "\nAll shards are healthy. Nothing to repair.")
		return nil
	}

	state, err := search.LoadState(indexDir)
	if err != nil {
		return err
	}

	removed, err := search.RepairIndex(indexDir, state)
	if err != nil {
		return fmt.Errorf("repair failed: %w", err)
	}

	if err := state.Save(indexDir); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: failed to save state: %v\n", err)
	}

	fmt.Fprintf(w, "\nRemoved %d corrupted shard(s):\n", len(removed))
	for _, p := range removed {
		fmt.Fprintf(w, "  %s\n", filepath.Base(p))
	}
	fmt.Fprintln(w, "\nRun 'ks index' to re-index affected repos.")
	return nil
}
