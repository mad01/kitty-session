# Command reference

Every subcommand exposed by the `ks` CLI, with flags and exit behavior.

`ks` uses [cobra](https://github.com/spf13/cobra) for argument parsing. Exit code is `0` on success and `1` on any error. Errors print to stderr; `SilenceUsage` is on, so cobra won't spam the usage block on error.

`ks` keeps its sessions in a kitty instance of its own, listening on the socket from `kitty_socket` in the config (default `~/.config/ks/kitty.sock`). Commands that create or focus tabs start that instance when it is not running; `close`, `rename`, `list` and `quit` never start it.

## `ks`

```
Usage: ks [--agent]
```

Attach. Starts the instance if its socket does not answer. Resumes every active session whose claude window is gone, 100 ms apart, and leaves stopped sessions alone. Then focuses the session you used last: the newest `focused_at`, the first active one by name if none was ever focused, the home tab when there is no active session. Prints one line:

```
ks: 2 resumed, 1 already running, 1 stopped
```

Sessions that fail to resume are reported as warnings on stderr; the attach continues past them. Two seconds after the last launch every relaunched claude window is checked again; one that is gone is not counted as resumed and gets its own line, `ks: <name> exited right after launch`. Running `ks` while everything is already up is a no-op apart from the focus.

### Flags

| Flag | Description |
|---|---|
| `--agent` | When this attach starts the instance, its home sidebar runs with `--agent`, so the background Haiku state monitor lives as long as the instance. With the instance already running the flag prints a note and does nothing. |

The `--agent` flag is persistent, so it's recognized on subcommands too; only `ks` (when starting the instance) and `ks sidebar` act on it.

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

1. Reads `~/.config/ks/config.yaml` (a missing file is fine).
2. Starts the instance if needed.
3. Writes `~/.config/ks/sessions/<name>.json` with `status: active`.
4. Creates a tab in the instance running `ks sidebar --session <name>`, switches it to the `splits` layout and titles it `<name>`.
5. Splits claude in beside the sidebar, moves the sidebar to the left edge and resizes it to `sidebar_width` cells. Both windows get `PATH`, `KS_SESSION_NAME` and `KS_SESSION_ID` in their environment and the kitty user variable `KS_SESSION_ID`; the Claude Code agent-session markers (`CLAUDECODE`, `CLAUDE_CODE_CHILD_SESSION`, `CLAUDE_CODE_SESSION_ID`, `CLAUDE_PID`, `CLAUDE_CODE_ENTRYPOINT`) are unset in both.
6. Focuses the claude window.
7. Writes the kitty IDs and `focused_at` back to the session file.

Claude Code asks whether you trust the files in a folder it has not seen before. The first thing a session in a new directory shows is that dialog; answer it once.

## `ks tmp`

```
Usage: ks tmp [-n <name>]
```

Create a session in a fresh scratch directory: `os.MkdirTemp(tmpdir, "ks-*")`, under `tmpdir` from the config or the OS temp dir. The name defaults to `tmp-<MMDD-HHMM>`, with a random suffix when that is taken. Every scratch directory is new to Claude Code, so each `ks tmp` session opens with the folder-trust dialog.

## `ks open <name>`

```
Usage: ks open <name>
```

Focus or recreate the named session. Starts the instance if needed.

- If the claude window is alive, focus it.
- If only the sidebar is left (claude exited or was closed), relaunch claude beside it in the same tab.
- Otherwise close whatever tab the session still owns and create the tab again. Claude starts with `--resume <id>` when the record has a `claude_session_id` whose transcript still exists, with `--continue` when the directory has any Claude transcript, and bare otherwise. A `--continue` with nothing to continue makes claude exit at once. The new kitty IDs are written back to the session file.

## `ks close <name>`

```
Usage: ks close <name> [--keep]
```

Close the session's tab. Works while the instance is down; the tab is then reported as left alone and the record is handled regardless.

### Flags

| Flag | Description |
|---|---|
| `--keep` | Keep the session file on disk, marked `stopped`, so it can be reopened later. Without this, the session file is moved to `~/.config/ks/sessions/trash/`. |

With `--keep`, only the tab goes away; `ks open <name>` recreates it. Without `--keep`, the record is trashed; restore it from the sidebar (`u` key).

## `ks list`

```
Usage: ks list
```

Print one line per session to stdout:

```
<name>               <state>    <dir>
```

State detection uses the same priority as the sidebar:

1. If the record is `stopped`, or no window tagged with the session's id matches its claude window → `stopped`.
2. If a fresh state file exists (written within the last 10 seconds by Claude Code hooks) → the value from the file.
3. Otherwise, read the claude window via `kitty @ get-text` and run the terminal-text classifier.

When the instance is not running every active session prints `stopped` and a last line says `ks instance not running`. Prints `no sessions` if no session files are found.

See [Hooks and state detection](hooks-and-state.md) for the full flow.

## `ks rename <old> <new>`

```
Usage: ks rename <old-name> <new-name>
```

Rename a session. Renames the session file, renames the state file if one exists, and retitles the tab when the session has one. Fails if `<new-name>` already exists. A tab title that cannot be set (instance down) is a warning. The sidebar in that tab keeps the old `--session` argument until the session is recreated.

## `ks quit`

```
Usage: ks quit
```

Close every window of the instance, which ends it. Session records stay `active`, so the next `ks` brings them all back; the command says how many:

```
ks instance closed; 3 active session(s) will resume on the next attach
```

Prints `ks instance not running` when there is nothing to close.

## `ks sidebar`

```
Usage: ks sidebar [--session <name>] [--agent]
```

Run the TUI in the current window. The instance runs one in its home tab (`ks sidebar`) and one on the left of every session tab (`ks sidebar --session <name>`); run it by hand to get the TUI in any terminal. The TUI talks to the instance on the configured socket without starting it, so outside the instance every session shows as `stopped`.

| Flag | Description |
|---|---|
| `--session` | The session whose tab this sidebar sits in. Shown in the title bar. |
| `--agent` | Start the background Haiku state monitor for as long as the sidebar runs. |

See [TUI guide](tui.md) for keybindings.

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
| `--toon` | [TOON](https://github.com/alpkeskin/gotoon) encoding, compact for LLM consumers. |

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

For each of `PreToolUse`, `Stop`, `Notification`, `SessionStart` and `SessionEnd`, `ks` installs a matcher group that invokes `<ks-binary> _hook`. The binary path is recorded with `$HOME` shortened to `~` for portability across machines.

Re-running `install` is idempotent: existing `ks` matcher groups are removed before new ones are written, so stale entries from an older binary path get cleaned up. Any non-`ks` hook entries are preserved.

## `ks hooks uninstall`

```
Usage: ks hooks uninstall
```

Reverse of `install`: strip every matcher group whose command resolves to `ks _hook` (either `~/...` or the absolute form) from `~/.claude/settings.json`. Events with no remaining groups are removed entirely. If that leaves `hooks` empty, the key is dropped.

## `ks _hook` (hidden)

Invoked by Claude Code hooks, not by humans. Reads a JSON payload from stdin, maps the `event` (and for `Notification`, the `type`) to one of `working` / `idle` / `input` / `waiting`, and writes `~/.config/ks/state/<name>.json`.

If `KS_SESSION_NAME` is not set, the command exits silently; it's safe to have the hook installed globally even in terminals that aren't `ks` sessions.

See [Hooks and state detection](hooks-and-state.md) for the full event-to-state table.

## Scripting recipes

### List sessions by name

```bash
ks list | awk '{print $1}'
```

### Bring everything back after a reboot

```bash
ks
```

### Inspect the instance

```bash
kitty @ --to unix:$HOME/.config/ks/kitty.sock ls
```

### Pipe repo picker into `cd` via a shell function

Covered in [Repo finder](repo-finder.md#shell-function).
