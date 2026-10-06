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

A kitty window titled `ks` appears: that is the instance. It holds a home tab and a `demo` tab. The tab is split in two: the `ks` sidebar on the left (36 cells wide) and Claude Code on the right, started in the chosen directory. There is no tab bar; the sidebar is the tab list. The first time Claude Code runs in a directory it asks whether you trust the folder; answer once.

You can also start from the sidebar. Run `ks`, and in the home tab's sidebar:

1. Press `n` to open the repo picker.
2. Type a few characters to filter, then `enter` to pick a repo.
3. `ks` derives a session name from the repo and branch, creates the tab, and starts Claude Code in the chosen directory.

## Move between sessions

In any sidebar:

- `j`/`k` or `↑`/`↓` to move the cursor, `1`-`9` to jump to a row.
- `enter` to focus that session's claude window (its tab is recreated if it is gone).
- `l`, `tab` or `q` to hand the keyboard to the claude window of the tab you are in; your kitty window keys (`cmd+]` / `cmd+[` in the dotfiles config) bring it back from claude.
- `c` to close the tab (the session record stays on disk, marked stopped).
- `d` to delete the session (the record moves to trash; see [Sidebar guide](tui.md#trash-and-restore)).

`m` opens a menu with every action, including `quit ks`.

## Come back later

```bash
ks
```

Bare `ks` attaches. It starts the instance if it is not running and brings back every active session whose claude window is gone, with `claude --resume` when the conversation's transcript is still there and `--continue` otherwise. Then it focuses the one you used last. `ks quit` closes the whole instance; the records stay, so the next `ks` restores them. `ks list` shows every session and its state from any terminal.

## Coming from herdr

If your Claude sessions live in [herdr](https://github.com/herdrdev/herdr), move them over with `ks import`. It reads herdr's default session file (`~/.config/herdr/session.json`), writes an active ks record for every claude pane, keyed by its Claude session id, and skips shells and other agents. Run `ks import --dry-run` first to see the names it would pick. Stop herdr before letting ks open the sessions, since a conversation can only have one claude attached. While herdr's daemon is still up, `ks import` writes the records but opens nothing and tells you to run `herdr session stop default` first. Details in the [command reference](commands.md#ks-import).

## Install Claude Code hooks (optional, recommended)

The sidebar shows a live state for each session: `input`, `done`, `working`, `idle` or `stopped`. The most accurate source for that state is Claude Code's own hook events, written by a hidden `ks _hook` handler. Wire them up once with:

```bash
ks hooks install
```

This edits `~/.claude/settings.json` to register `ks _hook` for `UserPromptSubmit`, `PreToolUse`, `Stop`, `Notification`, `SessionStart` and `SessionEnd`. Remove them with `ks hooks uninstall`.

See [Hooks and state detection](hooks-and-state.md) for what each event maps to and the fallback chain when hooks aren't installed.

## Next reads

- [Configuration](configuration.md): scratch tmp dirs, the instance socket, sidebar width, kitty overrides.
- [Sidebar guide](tui.md): rows, states, every key.
- [Command reference](commands.md): scripting with `ks new`, `ks list`, `ks quit`, `ks repo`.
