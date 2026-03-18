# kitty-session

Kitty Claude Session Manager — manage named kitty terminal sessions with Claude and a shell, using either a split layout (Claude on top, shell on bottom) or a tab layout (separate tabs).

## Screenshots

### Session list

The main view lists all sessions with their status and working directory.

![Session list](images/default.png)

### Create a new session

Press `n` to open the repository picker. Browse all configured repos or type to fuzzy-filter.

![Repository picker](images/repos.png)

![Filtering repos](images/repos-filter.png)

### Open a session

Select a session and press `o` to open it. Each session opens Claude Code and a shell — as a horizontal split (default) or as separate tabs.

![Running session](images/session.png)

### Help

Press `?` to see all available keybindings.

![Help overlay](images/help.png)

## Install

```bash
make install    # builds and copies ks to ~/code/bin/
```

## Usage

### TUI

```bash
ks              # launch interactive session manager
```

TUI keybindings:
- `j/k` Navigate sessions
- `o / enter` Open or focus session
- `n` Create new session (opens repo picker)
- `c` Close tab (keep session)
- `d` Delete session
- `/` Fuzzy search
- `?` Toggle help
- `q` Quit

### Subcommands

```bash
ks new -n <name> [-d <dir>]   # create session
ks open <name>                 # focus or recreate session
ks close <name> [--keep]       # close session tab
ks list                        # list all sessions
ks version                     # print version
ks repo                        # fuzzy repo finder
ks repo --list                 # list all repos
ks repo --json                 # list all repos as JSON
ks repo --toon                 # list all repos as TOON (token-optimized for LLMs)
```

### Shell function

Add to your shell config to jump to a repo:

```bash
repo() { local d=$(ks repo); [[ -n "$d" ]] && cd "$d"; }
```

## Config

Configuration lives in `~/.config/ks/config.yaml`:

```yaml
dirs:
  - ~/code/src/github.com/mad01
  - ~/workspace
layout: split  # "split" (default) or "tab"
tmpdir: ~/.config/ks/claude-session-workspaces  # optional
```

- `dirs` — parent directories to scan for repositories
- `layout` — `split` puts Claude and shell in a horizontal split (Claude on top 30%, shell on bottom 70%). `tab` creates separate kitty tabs for Claude and shell within the same OS window.
- `tmpdir` — base directory for scratch sessions created via the `tmp` picker item. Defaults to the OS temp directory when unset. Setting a custom path (e.g. `~/.config/ks/claude-session-workspaces`) keeps scratch workspaces in a predictable location that won't be cleaned up by the OS.
