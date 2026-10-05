# Getting started

This walks you through installing `ks`, writing the smallest useful config, and creating your first session.

## Prerequisites

- **[kitty](https://sw.kovidgoyal.net/kitty/)** on `PATH`. `ks` starts a kitty instance of its own with remote control enabled on a private socket (`~/.config/ks/kitty.sock`). Nothing in your `kitty.conf` has to change; the instance still reads it for fonts and colors. Your own kitty windows are never touched. Tested with kitty 0.48.

- **[Claude Code CLI](https://docs.claude.com/en/docs/claude-code/overview)** on `PATH`. Every session runs `claude`; `ks` forwards your `PATH` into the instance's windows so a `claude` installed under your home directory is found.

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

Tildes are expanded. See [Configuration](configuration.md) for `tmpdir`, `kitty_socket`, `sidebar_width` and `kitty_overrides`.

## Create your first session

```bash
ks new -n demo -d ~/code/src/github.com/you/demo
```

A kitty window titled `ks` appears: that is the instance. It holds a home tab and a `demo` tab. The tab is split in two: the sidebar on the left (the `ks` TUI, 36 cells wide) and Claude Code on the right, started in the chosen directory. There is no tab bar; the sidebar is the tab list.

You can also start from the TUI. Run `ks`, and in the home tab's sidebar:

1. Press `n` to open the repo picker.
2. Type a few characters to filter, then `enter` to pick a repo.
3. `ks` derives a session name from the repo and branch, creates the tab, and starts Claude Code in the chosen directory.

## Move between sessions

In any sidebar:

- `j`/`k` or `↑`/`↓` to move the selection.
- `enter` or `o` to focus the session's claude window.
- `c` to close the tab (the session record stays on disk for recovery).
- `d` to delete the session (the record moves to trash; see [TUI guide](tui.md#trash-and-restore)).

Press `?` at any time for the full keybinding list.

## Come back later

```bash
ks
```

Bare `ks` attaches. It starts the instance if it is not running and brings back every active session whose claude window is gone, with `claude --resume` when the conversation's transcript is still there and `--continue` otherwise. Then it focuses the one you used last. `ks quit` closes the whole instance; the records stay, so the next `ks` restores them. `ks list` shows every session and its state from any terminal.

## Install Claude Code hooks (optional, recommended)

The sidebar shows a live state for each session: `working`, `idle`, `input`, `waiting`, or `stopped`. The most accurate source for that state is Claude Code's own hook events, written by a hidden `ks _hook` handler. Wire them up once with:

```bash
ks hooks install
```

This edits `~/.claude/settings.json` to register `ks _hook` for `PreToolUse`, `Stop`, `Notification`, `SessionStart` and `SessionEnd`. Remove them with `ks hooks uninstall`.

See [Hooks and state detection](hooks-and-state.md) for what each event maps to and the fallback chain when hooks aren't installed.

## Next reads

- [Configuration](configuration.md): scratch tmp dirs, the instance socket, sidebar width, kitty overrides.
- [TUI guide](tui.md): every mode, every key.
- [Command reference](commands.md): scripting with `ks new`, `ks list`, `ks quit`, `ks repo`.
