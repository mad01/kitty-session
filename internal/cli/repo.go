package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alpkeskin/gotoon"
	fuzzyfinder "github.com/ktr0731/go-fuzzyfinder"
	"github.com/spf13/cobra"

	"github.com/mad01/kitty-session/internal/repo/config"
	"github.com/mad01/kitty-session/internal/repo/finder"
)

var (
	repoListFlag bool
	repoJSONFlag bool
	repoToonFlag bool
)

var repoCmd = &cobra.Command{
	Use:   "repo [query]",
	Short: "Interactive git repository finder",
	Long: "Scan configured directories for git repositories and pick one with a fuzzy finder.\n\n" +
		"With no query, opens an interactive picker.\n" +
		"With a query, prints the path of the single matching repo (case-insensitive substring on org/repo).\n" +
		"Errors out non-zero if the query matches zero or multiple repos.",
	Args: cobra.MaximumNArgs(1),
	RunE: runRepo,
}

func init() {
	repoCmd.Flags().BoolVar(&repoListFlag, "list", false, "list all repos (non-interactive)")
	repoCmd.Flags().BoolVar(&repoJSONFlag, "json", false, "output as JSON (implies --list)")
	repoCmd.Flags().BoolVar(&repoToonFlag, "toon", false, "output as TOON for LLMs (implies --list)")
	rootCmd.AddCommand(repoCmd)
}

type repoJSON struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Remote string `json:"remote,omitempty"`
	Host   string `json:"host,omitempty"`
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

	var query string
	if len(args) == 1 {
		query = args[0]
		repos = filterRepos(repos, query)
	}

	nonInteractive := repoJSONFlag || repoToonFlag || repoListFlag

	if !nonInteractive && query != "" {
		switch len(repos) {
		case 1:
			fmt.Fprintln(cmd.OutOrStdout(), repos[0].Path)
			return nil
		case 0:
			return fmt.Errorf("no repos match query %q", query)
		default:
			names := make([]string, len(repos))
			for i, r := range repos {
				names[i] = r.Name
			}
			return fmt.Errorf("multiple repos match query %q:\n  - %s", query, strings.Join(names, "\n  - "))
		}
	}

	if repoJSONFlag {
		items := make([]repoJSON, len(repos))
		for i, r := range repos {
			items[i] = repoJSON{Name: r.Name, Path: r.Path, Remote: r.Remote, Host: r.Host}
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}

	if repoToonFlag {
		items := make([]map[string]any, len(repos))
		for i, r := range repos {
			items[i] = map[string]any{"name": r.Name, "path": r.Path, "remote": r.Remote, "host": r.Host}
		}
		encoded, err := gotoon.Encode(map[string]any{"repos": items})
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), encoded)
		return nil
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

// filterRepos returns the subset of repos whose Name contains query (case-insensitive).
func filterRepos(repos []finder.Repo, query string) []finder.Repo {
	q := strings.ToLower(query)
	out := repos[:0:0]
	for _, r := range repos {
		if strings.Contains(strings.ToLower(r.Name), q) {
			out = append(out, r)
		}
	}
	return out
}
