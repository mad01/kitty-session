# Configuration

`ks` reads a single YAML file at `~/.config/ks/config.yaml`. There is no per-repo config and no environment override for the path.

If the file is missing, commands that need it (`ks repo`, the TUI repo picker, `ks new` when resolving layout) return an error pointing at the expected path.

## Schema

```yaml
dirs:               # required — parent directories to scan for repos
  - ~/code/src/github.com
  - ~/workspace
layout: split       # optional — "split" (default) or "tab"
summary: false      # optional — enable summary tab (requires layout: tab)
tmpdir:             # optional — base directory for scratch sessions
```

All keys are optional except `dirs`, which has no default. Tildes are expanded in `dirs` and `tmpdir`.

## Keys

### `dirs`

List of parent directories. `ks` does a concurrent breadth-first walk of each, stops at the first `.git` in any subtree, and records that directory as a repo. Directories starting with `.` are skipped.

Repositories are named from their `origin` remote URL. `ks` reads `.git/config` directly — no `git` subprocess. Both SSH (`git@host:org/repo.git`) and HTTPS (`https://host/org/repo.git`) formats are parsed. If there is no `origin` remote, `ks` falls back to `<parent>/<dir>`.

### `layout`

Controls how `ks new` and `ks open` arrange Claude Code and the shell inside a session's kitty tab.

- `split` (default): Claude runs in a top pane at 30% height; the shell runs in a bottom pane at 70%. Both live in the same kitty tab.
- `tab`: Claude runs in one kitty tab; the shell runs in a sibling tab inside the same OS window. Switch with kitty's own tab navigation.

Any value other than `tab` is treated as `split`.

### `summary`

Set to `true` to launch a third tab running a Haiku-powered summary agent whenever a session is created or recreated. The agent reads the Claude Code tab via `kitty @ get-text` and prints a structured status.

**Requires `layout: tab`.** With `layout: split` the flag is ignored — the TUI has no room to place a third pane.

See [Summary tab](summary-tab.md) for refresh cadence, model, and output format.

### `tmpdir`

Base directory used when you pick the `tmp` entry at the top of the repo picker. `ks` calls `os.MkdirTemp(tmpdir, "ks-*")` to create a scratch workspace, then opens a session rooted there.

When unset, the OS temp directory is used (`/tmp` on Linux, `/var/folders/...` on macOS). Those paths get cleaned up periodically. Setting `tmpdir: ~/.config/ks/claude-session-workspaces` keeps your scratch workspaces in a predictable, persistent location.

The directory is created if it doesn't exist.

## Full example

```yaml
dirs:
  - ~/code/src/github.com/mad01
  - ~/workspace

layout: tab
summary: true

tmpdir: ~/.config/ks/claude-session-workspaces
```

This gives you tab-style sessions, a persistent scratch directory, and a summary pane for every session.

## Storage layout

`ks` writes under `~/.config/ks/`:

- `config.yaml` — this file
- `sessions/<name>.json` — one per live session
- `sessions/trash/<name>.json` — deleted sessions (recovered from the TUI restore mode)
- `state/<name>.json` — state detection output (written by Claude Code hooks or the agent fallback)

Session files are safe to hand-edit if the stored `kitty_tab_id` goes stale and you want to re-associate by hand.
