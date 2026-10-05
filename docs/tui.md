# Sidebar guide

The sidebar is `ks sidebar`, a fixed-width [Bubble Tea](https://github.com/charmbracelet/bubbletea) view that lists every agent (session) with its state. The ks instance runs one in its home tab and one on the left of every session tab (`ks sidebar --session <name>`). The tab bar is hidden, so the sidebar is the tab list. Every session action the subcommands expose is available from here too.

## Launch

```bash
ks                    # attach; the instance opens with a sidebar in its home tab
ks sidebar            # the sidebar in the current terminal, by hand
ks sidebar --agent    # same, plus the background Haiku state monitor
ks _sidebar-demo      # the sidebar on fake agents; no kitty needed
```

Run by hand, `ks sidebar` needs the instance to be up and exits with `ks instance not running` otherwise. `--agent` is a fallback for when the Claude Code hooks are not installed; see [Hooks and state detection](hooks-and-state.md). `_sidebar-demo` is hidden and takes `--width` and `--session` to tune the preview.

## Rows

The frame is `sidebar_width` columns wide (default 36, see [Configuration](configuration.md)) and as tall as the window. Under the `agents … priority` header each agent takes two lines:

- A dot for the state, then the name. The cursor row has a highlight; the row of the tab the sidebar sits in carries a `▌` marker and its own background.
- Claude's tab title minus its state glyph (`Plan the merge`), or the session directory with `$HOME` shortened to `~` when Claude has not set one.

The footer has two labels, `new` and `menu`, and the line above it shows the result of the last action or its error until the next key. The list refreshes every three seconds and after every action; the `working` and `input` dots pulse.

## States

| Dot | State | Meaning |
|---|---|---|
| `●` pulsing red | `input` | Claude is waiting on you: a permission prompt or a question |
| `●` | `done` | Claude finished a turn and you have not looked at the tab since |
| `●` pulsing amber | `working` | Claude is processing |
| `○` | `idle` | Claude is at its prompt and the result has been seen |
| `·` | `stopped` | The record is stopped, or no claude window carries the session's tag |

Each row's state comes from one `kitty @ ls` snapshot plus the session's state file. First match wins:

1. Record `stopped`, or no claude window tagged with the session's id: `stopped`.
2. State file says `input` and is less than 10 s old: `input`.
3. Claude's title starts with a spinner glyph: `working`.
4. Claude's title starts with the idle glyph `✳`: `done` when the state file says `idle` with an `updated_at` newer than the record's `viewed_at`, else `idle`.
5. No glyph: the state file's `working` or `input` as is; `idle`, `waiting`, or no file at all: `idle`.

`viewed_at` is stamped by the session's own sidebar while its tab is the active one, at most every 10 s. So a turn that finishes while you are in another tab shows as `done` until you switch to it, and drops to `idle` within a few seconds of your looking. Without the hooks there is no state file, and `done` and `input` never appear; the title glyph still gives `working` and `idle`.

### Sort

Rows are ordered by state in the order of the table above, then by the state file's `updated_at` with the most recent first, then by name. The cursor stays on the same agent across refreshes.

## Keys

| Key | Action |
|---|---|
| `j` / `↓`, `k` / `↑` | Move the cursor |
| `1`-`9` | Jump to that row and focus it |
| `enter` | Focus the agent under the cursor; a tab that is gone is recreated |
| `l`, `tab`, `q` | Hand the keyboard to this tab's claude window |
| `n` | New agent: open the repo picker |
| `r` | Rename the agent under the cursor |
| `c` | Close its tab, keeping the record (asks first) |
| `d` | Delete it: close the tab and trash the record (asks first) |
| `u` | Restore a trashed session |
| `/` | Filter rows by name; `enter` keeps the filter, `esc` clears it |
| `m` | Open the menu |

`ctrl+c` does nothing: the sidebar never exits on its own. To end ks use the menu's `quit ks` or `ks quit`.

### Between the sidebar and claude

`l`, `tab` or `q` move the keyboard to the claude window of the tab you are in. From claude, the kitty chord `ctrl+b` then `s` moves it back to the sidebar, and `ctrl+b` then `a` returns to the agent. The chords are plain kitty mappings (`neighboring_window left` / `right`) the instance starts with, so they work in every tab. Claude Code also uses `ctrl+b` on its own, to move a running task to the background; inside the instance kitty takes the key first.

## Mouse

Left click selects and focuses a row, opens the picker from the `new` label, opens the menu from the `menu` label, and runs the menu entry under the pointer. A click outside the menu closes it.

## New agent (`n`)

The picker shows `tmp` first, then every git repository under the configured `dirs`, scanned while you type. Typing filters the list (fuzzy); `↑`/`↓` or `ctrl+p`/`ctrl+n` move, `enter` picks, `esc` goes back. `tmp` creates a scratch directory under `tmpdir`, like `ks tmp`.

The session name is the directory's base name plus the checked-out git branch, lower-cased with other characters collapsed to hyphens (`kitty-session-main`). When that name is free the tab is created at once. When it is taken, or a directory has no name to offer, a name prompt opens with the suggestion and the reason; `enter` creates, `esc` returns to the picker.

## Rename (`r`)

The cursor row turns into an input pre-filled with the current name. `enter` saves, `esc` cancels. The record, its state file and the tab title are renamed together.

## Close and delete (`c`, `d`)

Both pop a confirmation with the agent's name. `y` or `enter` confirms, `n` or `esc` cancels. Close removes the tab and marks the record `stopped`; `enter` on the row later brings it back. Delete also moves the record to `~/.config/ks/sessions/trash/`.

## Trash and restore (`u`)

A popup lists the trashed sessions; `j`/`k` move, `enter` restores, `esc` cancels. A restored session comes back `stopped`. `enter` on its row recreates the tab, with `claude --resume` when its transcript is still there and `--continue` otherwise.

## Menu (`m`)

A popup above the footer: `new agent`, `rename`, `close (keep)`, `delete`, `restore`, `shell split`, `hooks status`, `quit ks`. `j`/`k` move, `enter` runs the entry, `esc`, `m` or `q` close it.

- `shell split` opens a shell below this tab's claude window, in the session directory, taking roughly a third of the height. The home tab has no agent, so there the entry reports that instead.
- `hooks status` says whether the five Claude Code hook events are registered in `~/.claude/settings.json`, or which are missing.
- `quit ks` closes every window of the instance, like `ks quit`. Records stay active and come back on the next `ks`.

## Width

A kitty window resize keeps the split's proportions, not the sidebar's cell count. The sidebar notices its own new width and asks kitty to resize it back to `sidebar_width`; the frame itself never draws wider than the window.
