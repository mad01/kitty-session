# TUI guide

Running `ks` with no subcommand launches an interactive TUI built with [Bubble Tea](https://github.com/charmbracelet/bubbletea). Every session action the subcommands expose is available from here too.

## Launch

```bash
ks            # interactive session manager
ks --agent    # same, plus start a background Haiku state monitor
```

The `--agent` flag is a fallback for when you haven't installed the Claude Code hooks. See [Hooks and state detection](hooks-and-state.md).

## Session list (default mode)

Each row shows:

- **Name** — as stored on disk
- **State badge** — see below
- **Directory** — the session's working directory, with `$HOME` shortened to `~`
- **Context line** — the most recent Claude Code prompt for that directory (truncated to 60 chars), pulled from `~/.claude/projects/<dir>/sessions-index.json`

The list polls every three seconds and refreshes state. A separate 350 ms animation tick pulses the `working` and `input` badges.

### Keybindings

| Key | Action |
|---|---|
| `j` / `↓` | Move down |
| `k` / `↑` | Move up |
| `o` / `enter` | Open or focus the selected session |
| `n` | Create new session (opens the repo picker) |
| `r` | Rename selected session |
| `c` | Close tab; keep session file on disk |
| `d` | Delete session (move to trash) |
| `u` | Restore a previously deleted session |
| `/` | Start fuzzy filter over session names |
| `?` | Toggle full help overlay |
| `q` / `esc` | Quit (asks for confirmation) |

`ctrl+c` and `ctrl+d` show a status-bar hint instead of quitting.

### State badges

`ks` derives a state for each session and renders it as a colored badge.

| Badge | Meaning |
|---|---|
| `● working` (indigo, pulsing) | Claude is running a tool or otherwise busy |
| `◆ input` (amber, pulsing) | Claude is waiting on a permission prompt or `y/n` question |
| `idle` | Prompt `>` is the last non-empty line — ready for a new task |
| `waiting` | Welcome screen, post-task finish, or too early to tell |
| `stopped` | The kitty tab for this session no longer exists |
| `unknown` | State file exists but value wasn't recognized (should not normally appear) |

How that state gets computed is covered in [Hooks and state detection](hooks-and-state.md).

## Repo picker (`n`)

Press `n` to pick the directory for a new session.

- The first entry is always `tmp`, which creates a scratch directory via `os.MkdirTemp` under the configured `tmpdir` (see [Configuration](configuration.md)).
- Remaining entries are every git repo under your configured `dirs`, discovered via the repo finder.
- Start typing and the picker auto-enters filter mode; the fuzzy filter matches on repo name.
- `enter` picks, `esc` cancels (or clears the filter if one is active).

When you pick a repo, `ks` derives a default session name from the directory's base name plus the current git branch (for example, `kitty-session-main`). If that name doesn't already exist, the session is created immediately. If it does, you're dropped into the name input prompt to edit.

## Name input (after conflict)

Used only when the auto-generated name collides. `enter` confirms, `esc` cancels. Empty names are rejected.

## Rename (`r`)

Opens an inline input pre-filled with the current name. `enter` saves, `esc` cancels. `ks` renames the session file, renames the state file if present, and updates the kitty tab title.

## Confirm (`c` and `d`)

`c` (close) and `d` (delete) both pop a confirmation dialog. Close removes the kitty tab but keeps the session file. Delete closes the tab *and* moves the session file to `~/.config/ks/sessions/trash/`.

## Trash and restore (`u`)

Press `u` from the list view to switch into restore mode. The restore list shows every session currently in trash. `enter` or `o` restores the selected one; `esc` returns to the main list.

Restored sessions come back with their old `kitty_tab_id`, which is almost certainly stale by then. Open the session (`o`) to recreate its kitty tab; `ks` detects the gone tab and re-launches Claude with `claude --continue` in the original directory.

## Help (`?`)

A scrollable viewport overlay listing every keybinding. Any key that's not scroll-related exits the overlay.

## Quit (`q` or `esc`)

Pops a confirmation dialog. Pressing `y` quits; anything else returns to the list. This is also what `ctrl+c`/`ctrl+d` nudge you toward.

## Summary tab refresh

If you have `layout: tab` and `summary: true` in your config, the TUI also fires a `refresh\n` message to every session's summary tab every five minutes. See [Summary tab](summary-tab.md).
