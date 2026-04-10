package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/mad01/kitty-session/internal/mcpserver"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start the ks MCP stdio server for Claude Code and other MCP clients",
	Long: `Start an MCP (Model Context Protocol) stdio server that exposes ks
functionality as native tools for Claude Code and other MCP clients.

The server reads JSON-RPC requests from stdin and writes responses to stdout,
then exits when the client closes stdin. No daemon, no port — just subprocess
IPC spawned per session by the MCP client.

Tools exposed:
  ks_repo_lookup      Resolve a repo name to its local checkout path
  ks_search           Search code across locally checked-out repos
  ks_count            Count matches grouped by repo or language
  ks_read             Read a file from a named local repo
  ks_query_validate   Validate a zoekt query before running it

Register with Claude Code:
  claude mcp add --scope user ks -- ks mcp

Smoke test the stdio transport:
  printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"smoke","version":"0"}}}\n{"jsonrpc":"2.0","id":2,"method":"tools/list"}\n' | ks mcp`,
	RunE: runMCP,
}

func init() {
	rootCmd.AddCommand(mcpCmd)
}

func runMCP(_ *cobra.Command, _ []string) error {
	server := mcpserver.New(Version)
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintf(os.Stderr, "ks mcp: %v\n", err)
		return err
	}
	return nil
}
