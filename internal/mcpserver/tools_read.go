package mcpserver

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mad01/kitty-session/internal/repo/config"
	"github.com/mad01/kitty-session/internal/repo/finder"
)

// readInput is the typed input for the ks_read tool.
type readInput struct {
	Repo      string `json:"repo" jsonschema:"repo name (substring match against org/repo, e.g. \"tboi\", \"mad01/kitty-session\")"`
	File      string `json:"file" jsonschema:"file path relative to the repo root"`
	StartLine int    `json:"start_line,omitempty" jsonschema:"first line to return, 1-based; 0 or omit for start of file"`
	EndLine   int    `json:"end_line,omitempty" jsonschema:"last line to return, 1-based inclusive; 0 or omit for end of file"`
}

// readLine is one line in the ks_read result.
type readLine struct {
	Number int    `json:"number" jsonschema:"1-based line number in the source file"`
	Text   string `json:"text"`
}

// readOutput is the typed output of the ks_read tool.
type readOutput struct {
	Repo  string     `json:"repo" jsonschema:"the resolved repo name (canonical org/repo form)"`
	Path  string     `json:"path" jsonschema:"file path relative to the repo root"`
	Lines []readLine `json:"lines"`
}

func registerReadTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "ks_read",
		Description: "Read a file from a named local repo by repo name and relative path. " +
			"Use when you already know which repo holds a file and want to read a specific line range without first resolving the repo's absolute path via ks_repo_lookup. " +
			"Supply start_line / end_line (1-based inclusive) to slice; omit both to read the whole file.",
	}, handleRead)
}

func handleRead(
	_ context.Context,
	_ *mcp.CallToolRequest,
	in readInput,
) (*mcp.CallToolResult, readOutput, error) {
	if strings.TrimSpace(in.Repo) == "" {
		return nil, readOutput{}, fmt.Errorf("repo is required")
	}
	if strings.TrimSpace(in.File) == "" {
		return nil, readOutput{}, fmt.Errorf("file is required")
	}
	if in.StartLine < 0 || in.EndLine < 0 {
		return nil, readOutput{}, fmt.Errorf("start_line and end_line must be >= 0")
	}
	if in.StartLine > 0 && in.EndLine > 0 && in.StartLine > in.EndLine {
		return nil, readOutput{}, fmt.Errorf("start_line (%d) must be <= end_line (%d)", in.StartLine, in.EndLine)
	}

	cfg, err := config.Load()
	if err != nil {
		return nil, readOutput{}, fmt.Errorf("load ks config: %w", err)
	}

	repos, err := finder.Walk(cfg.Dirs)
	if err != nil {
		return nil, readOutput{}, fmt.Errorf("walk repos: %w", err)
	}

	var matched *finder.Repo
	for i := range repos {
		if strings.Contains(repos[i].Name, in.Repo) {
			matched = &repos[i]
			break
		}
	}
	if matched == nil {
		return nil, readOutput{}, fmt.Errorf("no repo matching %q found; try ks_repo_lookup first", in.Repo)
	}

	absPath := filepath.Join(matched.Path, in.File)
	f, err := os.Open(absPath)
	if err != nil {
		return nil, readOutput{}, fmt.Errorf("open %s: %w", absPath, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Increase the buffer so lines longer than 64 KB don't trip the scanner.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	lines := make([]readLine, 0, 256)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if in.StartLine > 0 && lineNum < in.StartLine {
			continue
		}
		if in.EndLine > 0 && lineNum > in.EndLine {
			break
		}
		lines = append(lines, readLine{Number: lineNum, Text: scanner.Text()})
	}
	if err := scanner.Err(); err != nil {
		return nil, readOutput{}, fmt.Errorf("read %s: %w", absPath, err)
	}

	return nil, readOutput{
		Repo:  matched.Name,
		Path:  in.File,
		Lines: lines,
	}, nil
}
