# Getting started

This walks you through installing `ks`, writing the smallest useful config, and creating your first session.

## Prerequisites

- **[kitty](https://sw.kovidgoyal.net/kitty/)** with remote control enabled. `ks` drives kitty through `kitty @` commands. Enable it once in `~/.config/kitty/kitty.conf`:

  ```
  allow_remote_control yes
  listen_on unix:/tmp/mykitty
  ```

  Restart kitty after editing. Without this, every command that launches or focuses a tab fails.

- **[Claude Code CLI](https://docs.claude.com/en/docs/claude-code/overview)** on `PATH`. `ks new` and `ks open` run `claude` in the new tab. The optional `--agent` flag and the summary tab also shell out to `claude`.

- **Go 1.25+** to build from source.

## Install

```bash
git clone https://github.com/mad01/kitty-session.git
cd kitty-session
make install
```

The `install` target builds `ks`, copies it to `~/code/bin/`, strips macOS quarantine attributes, and ad-hoc signs the binary so macOS stops complaining. Make sure `~/code/bin` is on your `PATH`, or edit the `Makefile` to copy somewhere else.

To build without installing:

```bash
make build        # produces ./ks in the repo
```

## Minimal config

Create `~/.config/ks/config.yaml`:

```yaml
dirs:
  - ~/code/src/github.com
```

`dirs` is a list of parent directories. `ks` recursively scans them for git repositories and stops descending at each `.git` it finds. Add as many roots as you want.

Tildes are expanded. See [Configuration](configuration.md) for `layout`, `summary`, and `tmpdir`.

## Create your first session

Launch the TUI:

```bash
ks
```

From the list view (which starts empty):

1. Press `n` to open the repo picker.
2. Type a few characters to filter, then `enter` to pick a repo.
3. `ks` derives a session name from the repo, creates a kitty tab, and starts Claude Code in the chosen directory. A shell opens beside it as a horizontal split (the default layout).

You land back in the TUI with the new session listed.

## Move between sessions

- `j`/`k` or `↑`/`↓` to move the selection.
- `enter` or `o` to focus the session's kitty tab.
- `c` to close the tab (the session record stays on disk for recovery).
- `d` to delete the session (the record moves to trash; see [TUI guide](tui.md#trash-and-restore)).

Press `?` at any time for the full keybinding list, or `q` to quit.

## Install Claude Code hooks (optional, recommended)

The list view shows a live state for each session: `working`, `idle`, `input`, `waiting`, or `stopped`. The most accurate source for that state is Claude Code's own hook events, written by a hidden `ks _hook` handler. Wire them up once with:

```bash
ks hooks install
```

This edits `~/.claude/settings.json` to register `ks _hook` for `PreToolUse`, `Stop`, `Notification`, and `SessionStart`. Remove them with `ks hooks uninstall`.

See [Hooks and state detection](hooks-and-state.md) for what each event maps to and the fallback chain when hooks aren't installed.

## Next reads

- [Configuration](configuration.md) — layout modes, scratch tmp dirs, summary tab.
- [TUI guide](tui.md) — every mode, every key.
- [Command reference](commands.md) — scripting with `ks new`, `ks list`, `ks repo`.
