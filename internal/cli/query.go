package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mad01/kitty-session/internal/search"
)

var (
	queryJSONFlag bool
)

var queryCmd = &cobra.Command{
	Use:   "query <pattern>",
	Short: "Validate and parse a search query",
	Long: `Validate a zoekt query and show its parsed tree.

Use this to debug query syntax errors before running a search.

Examples:
  ks query "func Walk"
  ks query "func \(Walk" --json
  ks query "repo:foo lang:go TODO"`,
	Args: cobra.ExactArgs(1),
	RunE: runQuery,
}

func init() {
	queryCmd.Flags().BoolVar(&queryJSONFlag, "json", false, "output as JSON")
	rootCmd.AddCommand(queryCmd)
}

func runQuery(cmd *cobra.Command, args []string) error {
	pattern := args[0]

	info := search.ValidateQuery(pattern)

	w := cmd.OutOrStdout()

	if queryJSONFlag {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(info)
	}

	if info.Valid {
		fmt.Fprintf(w, "valid: true\nparsed: %s\n", info.Parsed)
	} else {
		fmt.Fprintf(w, "valid: false\nerror: %s\n", info.Error)
		if info.Hint != "" {
			fmt.Fprintf(w, "hint: %s\n", info.Hint)
		}
	}

	return nil
}
