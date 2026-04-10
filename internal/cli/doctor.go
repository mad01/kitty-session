package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/mad01/kitty-session/internal/daemon"
	"github.com/mad01/kitty-session/internal/repo/config"
	"github.com/mad01/kitty-session/internal/repo/finder"
	"github.com/mad01/kitty-session/internal/search"
)

var (
	doctorJSONFlag bool
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check search index health",
	Long: `Check the health of the search index.

Reports issues (stale, missing, or dirty indexes) and lists healthy repos.
Use --json for machine-readable output.`,
	RunE: runDoctor,
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorJSONFlag, "json", false, "output as JSON")
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, args []string) error {
	indexDir, err := search.DefaultIndexDir()
	if err != nil {
		return err
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

	indexSize, _ := search.IndexDirSize(indexDir)

	// Validate shard integrity.
	shards, corrupted, _ := search.ValidateShards(indexDir)

	report := search.DoctorReport{
		IndexDir:       indexDir,
		IndexSizeBytes: indexSize,
		TotalRepos:     len(repos),
	}

	// Classify stale repos into issues.
	for _, r := range staleness.Stale {
		rs, indexed := state.GetRepo(r.Path)

		issue := search.RepoIssue{
			Repo: r.Name,
			Path: r.Path,
		}

		if !indexed {
			issue.Type = "missing"
			issue.Message = "not indexed"
		} else {
			fp, ok := staleness.Current[r.Path]
			if ok && fp.Dirty {
				issue.Type = "dirty"
				issue.Dirty = true
				mod, untracked, _ := search.DirtyInfo(r.Path)
				issue.ModifiedFiles = mod
				issue.UntrackedFiles = untracked
				issue.Message = fmt.Sprintf("%d modified, %d untracked", mod, untracked)
			} else {
				issue.Type = "stale"
				if !rs.IndexedAt.IsZero() {
					issue.IndexAge = time.Since(rs.IndexedAt).Truncate(time.Second).String()
				}
				issue.Message = "index out of date"
			}
		}

		report.Issues = append(report.Issues, issue)
	}

	// Classify fresh repos as healthy.
	for _, r := range staleness.Fresh {
		rs, _ := state.GetRepo(r.Path)
		age := "-"
		if !rs.IndexedAt.IsZero() {
			age = time.Since(rs.IndexedAt).Truncate(time.Second).String()
		}
		report.Healthy = append(report.Healthy, search.RepoHealth{
			Repo:     r.Name,
			Path:     r.Path,
			IndexAge: age,
		})
	}

	w := cmd.OutOrStdout()

	if doctorJSONFlag {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}

	// Daemon status
	daemonStatus := "stopped"
	if daemon.IsRunning(daemon.DefaultPIDPath()) {
		daemonStatus = "running"
	}

	// Shard health
	corruptedCount := len(corrupted)
	shardCount := len(shards)

	fmt.Fprintf(w, "Index directory: %s\n", report.IndexDir)
	fmt.Fprintf(w, "Index size:      %s\n", formatBytes(report.IndexSizeBytes))
	fmt.Fprintf(w, "Total repos:     %d\n", report.TotalRepos)
	fmt.Fprintf(w, "Index shards:    %d (%d healthy, %d corrupted)\n", shardCount, shardCount-corruptedCount, corruptedCount)
	fmt.Fprintf(w, "Search daemon:   %s\n", daemonStatus)
	fmt.Fprintln(w)

	if corruptedCount > 0 {
		fmt.Fprintf(w, "Corrupted shards (%d):\n", corruptedCount)
		for _, s := range shards {
			if !s.OK {
				fmt.Fprintf(w, "  %s — %s\n", s.Path, s.Error)
			}
		}
		fmt.Fprintln(w, "  Run 'ks index --repair' to remove corrupted shards, then 'ks index' to rebuild.")
		fmt.Fprintln(w)
	}

	if len(report.Issues) > 0 {
		fmt.Fprintf(w, "Issues (%d):\n", len(report.Issues))
		for _, issue := range report.Issues {
			fmt.Fprintf(w, "  %-8s %-40s %s\n", issue.Type, issue.Repo, issue.Message)
		}
		fmt.Fprintln(w)
	}

	if len(report.Healthy) > 0 {
		fmt.Fprintf(w, "Healthy (%d):\n", len(report.Healthy))
		for _, h := range report.Healthy {
			fmt.Fprintf(w, "  %-40s indexed %s ago\n", h.Repo, h.IndexAge)
		}
	}

	if len(report.Issues) == 0 && len(report.Healthy) > 0 {
		fmt.Fprintln(w, "\nAll repos are healthy.")
	}

	if len(report.Issues) == 0 && len(report.Healthy) == 0 {
		fmt.Fprintln(w, "No repos found. Run 'ks index' to build the index.")
	}

	return nil
}

func formatBytes(b int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case b >= gb:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
