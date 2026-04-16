# Command reference

Every subcommand exposed by the `ks` CLI, with flags and exit behavior.

`ks` uses [cobra](https://github.com/spf13/cobra) for argument parsing. Exit code is `0` on success and `1` on any error. Errors print to stderr; `SilenceUsage` is on, so cobra won't spam the usage block on error.

## `ks`

```
Usage: ks [flags]
```

Running `ks` with no subcommand launches the interactive TUI.

### Flags

| Flag | Description |
|---|---|
| `--agent` | Start a background Haiku agent that monitors session state via `kitty @ get-text`. Fallback for when Claude Code hooks aren't installed. Agent is killed (along with its process group) when the TUI exits. |

The `--agent` flag is persistent, so it's recognized on subcommands too, but only the root command's TUI path actually launches the agent.

See [TUI guide](tui.md) for keybindings.

## `ks new`

```
Usage: ks new -n <name> [-d <dir>]
```

Create a new session. Fails if a session with the same name already exists.

### Flags

| Flag | Required | Description |
|---|---|---|
| `-n`, `--name` | yes | Session name. Used as the kitty tab title and the state-file name. |
| `-d`, `--dir` | no | Working directory. Defaults to the current directory. Tildes are not expanded; pass an absolute path. |

Behavior:

1. Reads `~/.config/ks/config.yaml` for `layout`, `summary`, and `tmpdir` (missing config is non-fatal for this command).
2. Launches a new kitty OS window running `claude` in the target directory, with `KS_SESSION_NAME=<name>` exported.
3. Sets the kitty tab title to the session name.
4. Launches the shell pane — as a horizontal split (default) or as a sibling tab when `layout: tab`.
5. Launches the summary tab if `summary: true` and `layout: tab`.
6. Focuses the Claude pane.
7. Writes `~/.config/ks/sessions/<name>.json`.

## `ks open <name>`

```
Usage: ks open <name>
```

Focus or recreate the named session.

- If the kitty tab is still alive, focus its Claude pane (or the tab itself if the window ID wasn't recorded).
- If the tab is gone, recreate it: launch Claude with `--continue` to resume the most recent Claude conversation in that directory, re-create the shell pane and (if enabled) summary tab, then persist the new kitty IDs back to the session file.

## `ks close <name>`

```
Usage: ks close <name> [--keep]
```

Close the session's kitty tabs.

### Flags

| Flag | Description |
|---|---|
| `--keep` | Keep the session file on disk so it can be reopened later. Without this, the session file is moved to `~/.config/ks/sessions/trash/`. |

With `--keep`, only the kitty tabs go away — the record remains so `ks open <name>` can recreate it. Without `--keep`, the record is trashed; restore it from the TUI (`u` key).

## `ks list`

```
Usage: ks list
```

Print one line per session to stdout:

```
<name>               <state>    <dir>
```

State detection uses the same priority as the TUI:

1. If the kitty tab no longer exists → `stopped`.
2. If a fresh state file exists (written within the last 10 seconds by Claude Code hooks) → the value from the file.
3. Otherwise, read the Claude pane via `kitty @ get-text` and run the terminal-text classifier.

See [Hooks and state detection](hooks-and-state.md) for the full flow.

Prints `no sessions` if no session files are found.

## `ks rename <old> <new>`

```
Usage: ks rename <old-name> <new-name>
```

Rename a session. Renames the session file, renames the state file if one exists, and updates the kitty tab title.

Fails if `<new-name>` already exists.

## `ks repo`

```
Usage: ks repo [--list | --json | --toon]
```

Find a git repository under the `dirs` configured in `~/.config/ks/config.yaml`. Default mode is an interactive fuzzy finder; the selected repo's absolute path is printed to stdout.

### Flags

| Flag | Output |
|---|---|
| *(none)* | Interactive [go-fuzzyfinder](https://github.com/ktr0731/go-fuzzyfinder); prints the selected repo's path. |
| `--list` | TSV: `<name>\t<path>` per repo. |
| `--json` | JSON array with `name`, `path`, `remote`, and `host` (last two omitted when empty). |
| `--toon` | [TOON](https://github.com/alpkeskin/gotoon) encoding — compact for LLM consumers. |

When invoked in a non-TTY context (for example piped into `read`) the interactive mode still runs if stdin is a TTY. Use one of the flag modes for clean scripting. See [Repo finder](repo-finder.md) for the shell function and output format examples.

## `ks version`

```
Usage: ks version
```

Prints the version string. `make build` sets this to the short git commit SHA via `-ldflags`. When built without that flag (for example `go build ./cmd/ks`), it prints `dev`.

## `ks hooks install`

```
Usage: ks hooks install
```

Register `ks _hook` with Claude Code by editing `~/.claude/settings.json`. Creates the file (and directory) if missing. Writes back pretty-printed JSON with a trailing newline.

For each of `PreToolUse`, `Stop`, `Notification`, and `SessionStart`, `ks` installs a matcher group that invokes `<ks-binary> _hook`. The binary path is recorded with `$HOME` shortened to `~` for portability across machines.

Re-running `install` is idempotent — existing `ks` matcher groups are removed before new ones are written, so stale entries from an older binary path get cleaned up. Any non-`ks` hook entries are preserved.

## `ks hooks uninstall`

```
Usage: ks hooks uninstall
```

Reverse of `install`: strip every matcher group whose command resolves to `ks _hook` (either `~/...` or the absolute form) from `~/.claude/settings.json`. Events with no remaining groups are removed entirely. If that leaves `hooks` empty, the key is dropped.

## `ks _hook` (hidden)

Invoked by Claude Code hooks, not by humans. Reads a JSON payload from stdin, maps the `event` (and for `Notification`, the `type`) to one of `working` / `idle` / `input` / `waiting`, and writes `~/.config/ks/state/<KS_SESSION_NAME>.json`.

If `KS_SESSION_NAME` is not set, the command exits silently — it's safe to have the hook installed globally even in terminals that aren't `ks` sessions.

See [Hooks and state detection](hooks-and-state.md) for the full event-to-state table.

## Scripting recipes

### List sessions by name

```bash
ks list | awk '{print $1}'
```

### Open every session sequentially (for session health check)

```bash
ks list | awk '{print $1}' | while read name; do
  ks open "$name"
  sleep 1
done
```

### Pipe repo picker into `cd` via a shell function

Covered in [Repo finder](repo-finder.md#shell-function).
