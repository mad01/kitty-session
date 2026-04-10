package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mad01/kitty-session/internal/daemon"
	"github.com/mad01/kitty-session/internal/search"
)

var (
	countJSONFlag    bool
	countRepoFlag    string
	countLangFlag    string
	countGroupByFlag string
)

var countCmd = &cobra.Command{
	Use:   "count <pattern>",
	Short: "Count matches across indexed repositories",
	Long: `Count matches for a pattern across all indexed repositories.

Results can be grouped by repo or language. Without --group-by,
a single total count is returned.

Examples:
  ks count "TODO"
  ks count "func.*Error" --group-by repo
  ks count "import" --lang go --group-by language
  ks count "FIXME" --repo kitty-session --json`,
	Args: cobra.ExactArgs(1),
	RunE: runCount,
}

func init() {
	countCmd.Flags().BoolVar(&countJSONFlag, "json", false, "output as JSON")
	countCmd.Flags().StringVarP(&countRepoFlag, "repo", "r", "", "filter to repos matching this name")
	countCmd.Flags().StringVarP(&countLangFlag, "lang", "l", "", "filter to a specific language")
	countCmd.Flags().StringVar(&countGroupByFlag, "group-by", "", "group counts by: repo, language")
	rootCmd.AddCommand(countCmd)
}

type countJSON struct {
	Total   int                  `json:"total"`
	Results []search.CountResult `json:"results,omitempty"`
}

func runCount(cmd *cobra.Command, args []string) error {
	pattern := args[0]

	indexDir, err := search.DefaultIndexDir()
	if err != nil {
		return err
	}

	opts := search.CountOptions{
		Pattern:    pattern,
		RepoFilter: countRepoFlag,
		Lang:       countLangFlag,
		GroupBy:    countGroupByFlag,
	}

	// Try daemon first.
	socketPath := daemon.DefaultSocketPath()
	var results []search.CountResult
	var total int
	if err := daemon.EnsureDaemon(indexDir, socketPath); err == nil {
		results, total, err = daemon.CountVia(context.Background(), socketPath, opts)
		if err != nil {
			// Daemon failed — fall through to in-process.
			fmt.Fprintf(cmd.ErrOrStderr(), "daemon count failed, falling back: %v\n", err)
			results, total, err = search.Count(context.Background(), indexDir, opts)
			if err != nil {
				return err
			}
		}
	} else {
		var err error
		results, total, err = search.Count(context.Background(), indexDir, opts)
		if err != nil {
			return err
		}
	}

	w := cmd.OutOrStdout()

	if countJSONFlag {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(countJSON{Total: total, Results: results})
	}

	if countGroupByFlag != "" {
		for _, r := range results {
			fmt.Fprintf(w, "%-40s %d\n", r.Group, r.Count)
		}
		fmt.Fprintf(w, "\ntotal: %d\n", total)
	} else {
		fmt.Fprintf(w, "%d\n", total)
	}

	return nil
}
