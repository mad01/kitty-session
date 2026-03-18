package cli

import (
	"encoding/json"
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	fuzzyfinder "github.com/ktr0731/go-fuzzyfinder"
	"github.com/spf13/cobra"

	"github.com/mad01/kitty-session/internal/repo/config"
	"github.com/mad01/kitty-session/internal/repo/finder"
)

var (
	repoListFlag  bool
	repoJSONFlag  bool
	repoTableFlag bool
)

var repoCmd = &cobra.Command{
	Use:   "repo",
	Short: "Interactive git repository finder",
	Long:  "Scan configured directories for git repositories and pick one with a fuzzy finder.",
	RunE:  runRepo,
}

func init() {
	repoCmd.Flags().BoolVar(&repoListFlag, "list", false, "list all repos (non-interactive)")
	repoCmd.Flags().BoolVar(&repoJSONFlag, "json", false, "output as JSON (implies --list)")
	repoCmd.Flags().BoolVar(&repoTableFlag, "table", false, "output as formatted table (implies --list)")
	rootCmd.AddCommand(repoCmd)
}

type repoJSON struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func runRepo(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	repos, err := finder.Walk(cfg.Dirs)
	if err != nil {
		return err
	}

	if len(repos) == 0 {
		return fmt.Errorf("no git repositories found")
	}

	if repoJSONFlag {
		items := make([]repoJSON, len(repos))
		for i, r := range repos {
			items[i] = repoJSON{Name: r.Name, Path: r.Path}
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}

	if repoTableFlag {
		return renderRepoTable(cmd, repos)
	}

	if repoListFlag {
		w := cmd.OutOrStdout()
		for _, r := range repos {
			fmt.Fprintf(w, "%s\t%s\n", r.Name, r.Path)
		}
		return nil
	}

	idx, err := fuzzyfinder.Find(repos, func(i int) string {
		return repos[i].Name + " @ " + repos[i].Path
	})
	if err != nil {
		if err == fuzzyfinder.ErrAbort {
			return nil
		}
		return err
	}

	fmt.Fprintln(cmd.OutOrStdout(), repos[idx].Path)
	return nil
}

func renderRepoTable(cmd *cobra.Command, repos []finder.Repo) error {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7571F9"))
	cellStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#C1C6B2"))

	t := table.New().
		Headers("REPO", "PATH").
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("#636363"))).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			return cellStyle
		})

	for _, r := range repos {
		t.Row(r.Name, r.Path)
	}

	fmt.Fprintln(cmd.OutOrStdout(), t.Render())
	return nil
}
