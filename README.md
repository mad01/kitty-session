# kitty-session

`ks` is a session manager for [kitty](https://sw.kovidgoyal.net/kitty/). It creates named kitty tabs that pair [Claude Code](https://docs.claude.com/en/docs/claude-code/overview) with a shell, tracks their state, and lets you jump between them from an interactive TUI or shell scripts.

## Screenshots

### Session list

The main TUI view lists every session with its live state and working directory.

![Session list](images/default.png)

### Create a session

Press `n` to open the repository picker. Browse everything `ks` has scanned or type to fuzzy-filter.

![Repository picker](images/repos.png)
![Filtering repos](images/repos-filter.png)

### Inside a session

Each session runs Claude Code and a shell in the same tab, either split horizontally (default) or as separate tabs.

![Running session](images/session.png)

### Help overlay

Press `?` for the full keybindings.

![Help overlay](images/help.png)

## Install

You need [kitty](https://sw.kovidgoyal.net/kitty/) with remote control enabled and the [Claude Code CLI](https://docs.claude.com/en/docs/claude-code/overview) on `PATH`. Go 1.25 or later is required to build from source.

```bash
make install
```

This builds `ks` and copies it to `~/code/bin/`. Adjust the `Makefile` or copy the binary yourself if you prefer another location.

For everything else — first session, configuration, subcommands, hooks — see the docs below.

## Documentation

- [Getting started](docs/getting-started.md) — install, minimal config, first session
- [Configuration](docs/configuration.md) — `~/.config/ks/config.yaml` reference
- [TUI guide](docs/tui.md) — modes, keybindings, state badges, trash and restore
- [Command reference](docs/commands.md) — every subcommand and flag
- [Repo finder](docs/repo-finder.md) — `ks repo` and its output formats
- [Hooks and state detection](docs/hooks-and-state.md) — how `ks` knows what Claude is doing
- [Summary tab](docs/summary-tab.md) — the optional Haiku-powered session summary
- [Architecture](docs/architecture.md) — package layout, data flow, extending `ks`
- [Troubleshooting](docs/troubleshooting.md) — common failures and fixes

## Code search

The MCP server, search daemon, and zoekt integration that used to live in this repo now live in [code-search-local (`csl`)](https://github.com/mad01/code-search-local). `ks` kept the `repo` subcommand so the shell `repo()` helper keeps working.
