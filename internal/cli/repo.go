package cli

import (
	"fmt"

	fuzzyfinder "github.com/ktr0731/go-fuzzyfinder"
	"github.com/spf13/cobra"

	"github.com/mad01/kitty-session/internal/repo/config"
	"github.com/mad01/kitty-session/internal/repo/finder"
)

var repoListFlag bool

var repoCmd = &cobra.Command{
	Use:   "repo",
	Short: "Interactive git repository finder",
	Long:  "Scan configured directories for git repositories and pick one with a fuzzy finder.",
	RunE:  runRepo,
}

func init() {
	repoCmd.Flags().BoolVar(&repoListFlag, "list", false, "list all repos (non-interactive)")
	rootCmd.AddCommand(repoCmd)
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
