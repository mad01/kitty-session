package mcpserver

import (
	"context"
	"fmt"
	"regexp"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mad01/kitty-session/internal/repo/config"
	"github.com/mad01/kitty-session/internal/repo/finder"
)

// repoLookupInput is the typed input for the ks_repo_lookup tool.
type repoLookupInput struct {
	Name string `json:"name" jsonschema:"case-insensitive regex or substring matched against the repo name (e.g. \"tboi\", \"mad01/.*\")"`
}

// repoMatch is one entry in the ks_repo_lookup result.
type repoMatch struct {
	Name string `json:"name" jsonschema:"org/repo name extracted from the git remote URL"`
	Path string `json:"path" jsonschema:"absolute filesystem path to the repo root"`
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
			matches = append(matches, repoMatch{Name: r.Name, Path: r.Path})
		}
	}

	return nil, repoLookupOutput{Matches: matches}, nil
}
