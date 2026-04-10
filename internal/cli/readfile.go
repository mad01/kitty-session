package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mad01/kitty-session/internal/repo/config"
	"github.com/mad01/kitty-session/internal/repo/finder"
)

var (
	readfileRepoFlag      string
	readfileStartLineFlag int
	readfileEndLineFlag   int
	readfileJSONFlag      bool
)

var readfileCmd = &cobra.Command{
	Use:   "read <file>",
	Short: "Read a file from a repository",
	Long: `Read a file from a repository with line numbers.

The --repo flag is required to resolve which repository the file belongs to.
The file path is relative to the repository root.

Examples:
  ks read internal/cli/root.go --repo kitty-session
  ks read main.go --repo kitty-session --start-line 10 --end-line 30
  ks read README.md --repo kitty-session --json`,
	Args: cobra.ExactArgs(1),
	RunE: runReadfile,
}

func init() {
	readfileCmd.Flags().StringVarP(&readfileRepoFlag, "repo", "r", "", "repository name to read from (required)")
	_ = readfileCmd.MarkFlagRequired("repo")
	readfileCmd.Flags().IntVar(&readfileStartLineFlag, "start-line", 0, "first line to show (1-based, 0 = start of file)")
	readfileCmd.Flags().IntVar(&readfileEndLineFlag, "end-line", 0, "last line to show (1-based, 0 = end of file)")
	readfileCmd.Flags().BoolVar(&readfileJSONFlag, "json", false, "output as JSON")
	rootCmd.AddCommand(readfileCmd)
}

type readfileLineJSON struct {
	Number int    `json:"number"`
	Text   string `json:"text"`
}

type readfileJSON struct {
	Repo  string             `json:"repo"`
	Path  string             `json:"path"`
	Lines []readfileLineJSON `json:"lines"`
}

func runReadfile(cmd *cobra.Command, args []string) error {
	relPath := args[0]

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	repos, err := finder.Walk(cfg.Dirs)
	if err != nil {
		return err
	}

	// Find the matching repo.
	var matched *finder.Repo
	for _, r := range repos {
		if strings.Contains(r.Name, readfileRepoFlag) {
			matched = &r
			break
		}
	}
	if matched == nil {
		return fmt.Errorf("no repo matching %q found", readfileRepoFlag)
	}

	absPath := filepath.Join(matched.Path, relPath)

	f, err := os.Open(absPath)
	if err != nil {
		return fmt.Errorf("cannot open %s: %w", absPath, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var lines []readfileLineJSON
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if readfileStartLineFlag > 0 && lineNum < readfileStartLineFlag {
			continue
		}
		if readfileEndLineFlag > 0 && lineNum > readfileEndLineFlag {
			break
		}
		lines = append(lines, readfileLineJSON{Number: lineNum, Text: scanner.Text()})
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading %s: %w", absPath, err)
	}

	w := cmd.OutOrStdout()

	if readfileJSONFlag {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(readfileJSON{
			Repo:  matched.Name,
			Path:  relPath,
			Lines: lines,
		})
	}

	for _, l := range lines {
		fmt.Fprintf(w, "%6d\t%s\n", l.Number, l.Text)
	}

	return nil
}
