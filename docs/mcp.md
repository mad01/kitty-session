# `ks mcp` — MCP stdio server reference

`ks mcp` is a [Model Context Protocol](https://modelcontextprotocol.io) stdio server bundled into the `ks` binary. It exposes the same search, repo-lookup, and file-read functionality that the cobra subcommands use, but as native MCP tools that Claude Code (and other MCP clients) can call without the user having to build shell strings or approve individual `Bash()` invocations.

There is no long-running server. The MCP client spawns `ks mcp` as a subprocess per session, pipes JSON-RPC 2.0 over stdin/stdout, and the process exits when stdin closes. See the [blog post](https://dropbrain.studio/blog/mcp-stdio-tools-not-services/) for the "tools, not services" background.

## Architecture

```
┌──────────────┐  spawn        ┌──────────┐  unix socket   ┌─────────────┐
│ Claude Code  │ ────────────▶ │  ks mcp  │ ─────────────▶ │ ks daemon   │
│  (client)    │ ◀──JSON-RPC── │ (stdio)  │ ◀──gRPC───────│  (zoekt)    │
└──────────────┘               └──────────┘                └─────────────┘
```

- `ks mcp` is a thin adapter over the internal Go packages that already back the cobra commands (`internal/search`, `internal/daemon`, `internal/repo/finder`, `internal/repo/config`).
- `ks_search` and `ks_count` follow the same daemon-first-then-fallback pattern as the CLI: they call `daemon.EnsureDaemon`, then `daemon.SearchVia` / `daemon.CountVia`, and fall back to in-process search if the daemon is unreachable. The daemon keeps zoekt shards mmap'd across calls, so each tool invocation is fast after the first.
- `ks_repo_lookup` and `ks_read` hit the filesystem directly via `finder.Walk` and do not talk to the daemon.

## Registration

```bash
claude mcp add --scope user ks -- ks mcp
```

This writes a user-scoped entry into Claude Code's config so the `ks_*` tools are available in every new session on the current machine. To sync registration across machines, use the `claude-mcp` recipe in [mad01/dotfiles](https://github.com/mad01/dotfiles) which reconciles `servers.json` against `claude mcp list`.

Verify after registering:

```bash
claude mcp list
# expect: ks: ks mcp - ✓ Connected
```

## Tools

### `ks_repo_lookup`

Resolve a git repo name to its absolute local checkout path.

**When Claude uses it:** the user mentions a repo by name and Claude needs its absolute path before `cd`-ing, reading, or grepping inside it.

**Input:**

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Case-insensitive regex or substring matched against the `org/repo` name (e.g. `tboi`, `mad01/.*`) |

**Output:**

| Field | Type | Description |
|---|---|---|
| `matches` | array | Matching repos; empty if no local checkout was found |
| `matches[].name` | string | `org/repo` name extracted from the git remote URL |
| `matches[].path` | string | Absolute filesystem path to the repo root |

**Example:**

```json
// request
{"name": "tboi"}

// response
{"matches": [{"name": "mad01/tboi", "path": "/Users/alex/code/src/github.com/mad01/tboi"}]}
```

Empty `matches` means the repo is not checked out locally. Claude is instructed to report that to the user rather than guess a path.

### `ks_search`

Search code across locally checked-out repos using zoekt query syntax.

**When Claude uses it:** "where is X defined", "find all Y", "does any of my projects use Z", "show me every TODO in the Go code".

**Input:**

| Field | Type | Default | Description |
|---|---|---|---|
| `query` | string | — | Zoekt query (see syntax below) |
| `repo` | string | — | Restrict to repo names matching this regex |
| `lang` | string | — | Restrict to files of this language (e.g. `go`, `swift`, `python`) |
| `file` | string | — | Restrict to file paths matching this regex (e.g. `\.go$`) |
| `output_mode` | string | `files_with_matches` | `files_with_matches` returns unique file paths; `content` returns matching lines |
| `context_lines` | int | `0` | Context lines around each match; content mode only |
| `limit` | int | `50` | Maximum number of file results |
| `case_sensitive` | bool | `false` | Force case-sensitive matching; default is smart case |

**Output (files_with_matches mode):**

```json
{
  "output_mode": "files_with_matches",
  "files": [
    {"repo": "mad01/kitty-session", "path": "internal/mcpserver/server.go"}
  ],
  "total": 1
}
```

**Output (content mode):**

```json
{
  "output_mode": "content",
  "lines": [
    {
      "repo": "mad01/kitty-session",
      "path": "internal/mcpserver/server.go",
      "line": 12,
      "column": 6,
      "text": "func New(version string) *mcp.Server {",
      "before": "...",
      "after": "..."
    }
  ],
  "total": 1
}
```

**Query syntax (zoekt):**

| Syntax | Meaning |
|---|---|
| `foo` | Literal substring |
| `"foo bar"` | Quoted phrase |
| `foo bar` | AND (space-separated) |
| `foo\|bar` | OR |
| `-foo` | NOT |
| `file:\.go$` | File path regex |
| `lang:go` | Language filter |
| `repo:kitty` | Repo name regex |
| `case:yes` | Case-sensitive match |

Run `ks search --help` or use `ks_query_validate` to experiment with complex queries.

### `ks_count`

Count matches of a zoekt query across repos, optionally grouped by repo or language.

**When Claude uses it:** cross-repo tallies like "how many TODOs across my Go projects" or "which language has the most calls to `fmt.Errorf`".

**Input:**

| Field | Type | Description |
|---|---|---|
| `query` | string | Zoekt query (same syntax as `ks_search`) |
| `repo` | string | Restrict to repo names matching this regex |
| `lang` | string | Restrict to files of this language |
| `group_by` | string | `repo`, `language`, or empty for a single total |

**Output:**

```json
{
  "total": 142,
  "groups": [
    {"group": "mad01/kitty-session", "count": 87},
    {"group": "mad01/dropbrain-studio", "count": 55}
  ]
}
```

`groups` is empty when `group_by` is not set.

### `ks_read`

Read a file from a named local repo by repo name and relative path.

**When Claude uses it:** Claude already knows which repo holds a file and wants a specific line range without first resolving the absolute path via `ks_repo_lookup`.

**Input:**

| Field | Type | Required | Description |
|---|---|---|---|
| `repo` | string | yes | Substring match against the `org/repo` name |
| `file` | string | yes | File path relative to the repo root |
| `start_line` | int | no | First line to return, 1-based; 0 or omit for start of file |
| `end_line` | int | no | Last line to return, 1-based inclusive; 0 or omit for end of file |

**Output:**

```json
{
  "repo": "mad01/kitty-session",
  "path": "internal/mcpserver/server.go",
  "lines": [
    {"number": 12, "text": "func New(version string) *mcp.Server {"}
  ]
}
```

### `ks_query_validate`

Validate a zoekt query and return its parsed tree or a parse error with a fixing hint.

**When Claude uses it:** to debug a complex query before invoking `ks_search`, especially when escaped regex metacharacters are involved.

**Input:**

| Field | Type | Description |
|---|---|---|
| `query` | string | The zoekt query string to validate |

**Output:**

```json
{
  "valid": true,
  "parsed": "(and substr:\"foo\" file_regexp:\"\\.go$\")"
}
```

On parse errors:

```json
{
  "valid": false,
  "error": "unexpected end of query",
  "hint": "check for unbalanced quotes or parentheses"
}
```

## Smoke test

To verify `ks mcp` speaks JSON-RPC correctly without involving Claude Code:

```bash
( printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"smoke","version":"0"}}}\n{"jsonrpc":"2.0","method":"notifications/initialized"}\n{"jsonrpc":"2.0","id":2,"method":"tools/list"}\n'; sleep 1 ) | ks mcp
```

Expected: one `initialize` response followed by a `tools/list` response listing the five `ks_*` tools with their input and output schemas. The `sleep 1` keeps stdin open long enough for the server to flush responses before exit.

## Troubleshooting

**`claude mcp list` shows `ks: ks mcp - ✗ Failed to connect`**
- Check that `ks` is on `PATH` for the Claude Code process (`which ks`). The registration stores the command name, not an absolute path, so a shell-local `PATH` will not work.
- Run the smoke test above. If it exits non-zero, the binary itself is broken — rebuild with `make install`.

**First `ks_search` call takes tens of seconds**
- The first search in a session triggers zoekt indexing of every configured repo. Subsequent calls reuse the daemon's in-memory shards and return in hundreds of milliseconds.
- To pre-warm manually: run `ks search foo` in a terminal before opening Claude Code.

**`ks_search` returns empty for a query that should match**
- Verify the indexed repos match your expectation: `ks doctor` shows the index state and the configured directories.
- Rebuild the index: `ks index` (or `ks index --force`).
- Test the query directly against the daemon: `ks search "<query>"` on the command line.

**`ks_repo_lookup` returns empty matches**
- The repo is not checked out under any of the directories listed in `~/.config/ks/config.yaml` `dirs`. Either clone it there or add the parent directory to `dirs`.
