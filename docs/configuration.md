# Configuration

`ks` reads a single YAML file at `~/.config/ks/config.yaml`. There is no per-repo config and no environment override for the path.

If the file is missing, `ks repo` and the sidebar's repo picker return an error pointing at the expected path. Every other command runs with the defaults below.

## Schema

```yaml
dirs:                     # parent directories to scan for repos
  - ~/code/src/github.com
  - ~/workspace
tmpdir:                   # optional — base directory for scratch sessions
kitty_socket: ~/.config/ks/kitty.sock   # optional — the instance's remote-control socket
sidebar_width: 36         # optional — sidebar width in cells, minimum 20
kitty_overrides:          # optional — extra kitty settings for the instance
  - font_size=13
```

Tildes are expanded in `dirs`, `tmpdir` and `kitty_socket`.

## Keys

### `dirs`

List of parent directories. `ks` does a concurrent breadth-first walk of each, stops at the first `.git` in any subtree, and records that directory as a repo. Directories starting with `.` are skipped.

Repositories are named from their `origin` remote URL. `ks` reads `.git/config` directly, no `git` subprocess. Both SSH (`git@host:org/repo.git`) and HTTPS (`https://host/org/repo.git`) formats are parsed. If there is no `origin` remote, `ks` falls back to `<parent>/<dir>`.

### `tmpdir`

Base directory used by `ks tmp` and the `tmp` entry at the top of the repo picker. `ks` calls `os.MkdirTemp(tmpdir, "ks-*")` to create a scratch workspace, then opens a session rooted there.

When unset, the OS temp directory is used (`/tmp` on Linux, `/var/folders/...` on macOS). Those paths get cleaned up periodically. Setting `tmpdir: ~/.config/ks/claude-session-workspaces` keeps your scratch workspaces in a predictable, persistent location.

The directory is created if it doesn't exist. Claude Code shows its folder-trust dialog the first time it runs in a directory, so every scratch session opens with that prompt.

### `kitty_socket`

Path of the unix socket the ks kitty instance listens on. `ks` passes it to kitty as `--listen-on unix:<path>` when starting the instance and as `--to unix:<path>` on every remote-control call. Default: `~/.config/ks/kitty.sock`. Relative paths are resolved against the working directory of the command that reads them, so use an absolute path or a tilde.

Changing it while an instance is running leaves that instance unreachable to `ks`; run `ks quit` first.

### `sidebar_width`

Width in cells of the sidebar window on the left of every session tab. Default 36. Values below 20 are raised to 20, the narrowest the sidebar can render. Applied when a tab is created or claude is relaunched into it; resizing the kitty window later keeps the split's proportions, not the cell count.

### `kitty_overrides`

List of `key=value` kitty settings appended to the instance's command line as `-o key=value`, after the settings `ks` needs. Use it for appearance: `font_size`, `background_opacity`, a theme `include`. The instance also reads your `~/.config/kitty/kitty.conf`, so most of the time nothing is needed here.

Entries without `=` are a config error. Overriding one of the settings `ks` sets (`allow_remote_control`, `tab_bar_style`, `window_border_width`, `window_margin_width`, `window_padding_width`, `macos_quit_when_last_window_closed`) is possible, since later `-o` flags win, and unsupported.

### `layout` and `summary` (deprecated)

Older configs set `layout: split | tab` and `summary: true`. Both are still parsed so the file loads, and both are ignored: every session has the one sidebar-plus-claude layout, and there is no summary window.

## Full example

```yaml
dirs:
  - ~/code/src/github.com/mad01
  - ~/workspace

tmpdir: ~/.config/ks/claude-session-workspaces

sidebar_width: 40
kitty_overrides:
  - font_size=12
```

## Storage layout

`ks` writes under `~/.config/ks/`:

- `config.yaml`: this file
- `kitty.sock`: the instance's socket (or wherever `kitty_socket` points); kitty removes it when the instance exits, and `ks` removes a stale one before starting a new instance
- `sessions/<name>.json`: one per live session
- `sessions/trash/<name>.json`: deleted sessions (recovered from the sidebar's restore mode)
- `state/<name>.json`: state detection output (written by Claude Code hooks or the agent fallback)

Session files are safe to hand-edit. The kitty ids in them are only trusted when a window in the instance carries the matching `KS_SESSION_ID` user variable. A wrong id makes `ks open` recreate the tab rather than touch someone else's.
