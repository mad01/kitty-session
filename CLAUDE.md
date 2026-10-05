# CLAUDE.md — kitty-session

`ks` is a session manager for the [kitty](https://sw.kovidgoyal.net/kitty/) terminal. It runs a kitty instance of its own. Every Claude Code session is one tab in it: a sidebar (the Bubble Tea TUI) on the left and claude on the right. The tab bar is hidden; the sidebar is the tab list. It tracks each session's state and brings every session back on attach. The user's own kitty is never touched. `README.md` is the front door; the `docs/` set is the deep dive. This file is the agent-facing working context: where code lives, how the on-disk model works, and the gotchas that bite.

Go module `github.com/mad01/kitty-session`, builds to a single `ks` binary. Needs Go 1.25+.

## Where things live

```
cmd/ks/main.go              entry point — calls internal/cli
internal/cli/               cobra subcommands (attach = bare ks, new, open, close, list, rename, quit, sidebar, tmp, repo, hooks, _hook, version)
internal/tui/               Bubble Tea TUI, run as `ks sidebar`
internal/instance/          the ks kitty instance: Ensure (ping or start), Connect, Client, Shutdown
internal/launcher/          Launcher: Open/Close/Rename/Attach/Alive; tab topology behind a backend interface
internal/kitty/client.go    the ONLY place that runs `kitty`: Client (every `kitty @ --to <socket>` call) and Start
internal/session/           Session struct + Store (save/load/list/delete/rename/restore)
internal/state/file.go      state JSON read/write + freshness predicates
internal/claude/            terminal-text classifier (DetectState) + Claude projects reader (LatestPrompt)
internal/procinfo/          parent/comm lookup for the hook's nested-claude guard
internal/repo/config/       ~/.config/ks/config.yaml loader
internal/repo/finder/       concurrent BFS repo walker + remote-URL parser
```

`internal/cli` depends on almost everything; `internal/tui` is its second consumer. `instance` and `launcher` are the two mid-layer packages: `instance` owns the kitty process, `launcher` everything inside it. Leaf packages have a single responsibility and don't import their peers. See `docs/architecture.md` for the package graph and the create-session, attach and detect-state flows.

**`internal/kitty` is the one exec boundary.** `kitty.New(socket)` returns a `Client`; each method wraps one `kitty @` subcommand and always passes `--to <socket>`. `Client.Start` is the only call that runs the kitty binary itself (`kitty --detach --listen-on ...` plus the `-o` overrides the topology depends on). Nothing else in the tree calls `exec.Command("kitty", ...)`. If you need a new kitty interaction, add a method to `client.go`. The repo finder also avoids subprocesses: it parses `.git/config` directly, never running `git`.

**Which wiring to use in a command.** `ensureWiring` (in `internal/cli/wiring.go`) starts the instance when it is down; `new`, `open`, `tmp` and attach use it. `offlineWiring` never starts it; `close`, `rename` and `list` use it so they work while the instance is down. Both hand back the store, the config (nil when the file is absent) and a `Launcher`.

## On-disk model

Everything `ks` writes lives under `~/.config/ks/`:

```
~/.config/ks/
├── config.yaml            user-authored (the only config; no per-repo, no env override for the path)
├── kitty.sock             the instance's remote-control socket (kitty_socket in config)
├── sessions/<name>.json   one per live session
├── sessions/trash/<name>.json   moved here by `ks close` (no --keep) and TUI delete
└── state/<name>.json      written by hooks or the --agent monitor
```

A session file holds the kitty IDs (`kitty_tab_id`, `kitty_window_id` for claude, `kitty_sidebar_window_id`), the working dir, a created-at stamp, `status`, the Claude session id and transcript path from the last `SessionStart` hook, and `focused_at`. **Kitty IDs restart from 1 in every instance**, so after `ks quit` a stored id can point at another session's window. The launcher therefore tags both windows of a session with the kitty user variable `KS_SESSION_ID=<id>` (`launch --var`, read back as `user_vars` in `kitty @ ls`) and only trusts an id when the tag matches. A record with no `id` owns nothing. Records from older versions may carry `kitty_shell_window_id` / `kitty_summary_window_id`; they are cleared on the next reopen.

State files are `{"state": "...", "updated_at": "..."}` where state is `working` / `idle` / `input` / `waiting`. Two freshness thresholds live in `internal/state/file.go`: `freshness = 10s` (trusted outright) and `IsRecentlyWorking = 5min` (a stale `working` is still honored unless terminal text clearly says idle or input).

## State detection — three sources, in preference order

1. **Claude Code hooks** (preferred, most accurate). `ks hooks install` registers a hidden `ks _hook` handler for `PreToolUse`→`working`, `Stop`→`idle`, `Notification`→`input`, `SessionStart`→`waiting` (plus `SessionEnd` for the stopped status) in `~/.claude/settings.json`. The handler keys off `KS_SESSION_ID` / `KS_SESSION_NAME`, which the launcher exports into both windows of a session. Unset env → handler exits silently, so the hook is safe to leave installed globally.
2. **Background Haiku agent** (optional fallback). `ks sidebar --agent` runs a long-running `claude` with a tight allow-list that polls `kitty @ --to <socket> get-text` every 5s and writes state files; `ks --agent` passes the flag to the home sidebar when it starts the instance. Killed with its process group when that sidebar exits.
3. **Terminal-text heuristic** (always-on). `internal/claude.DetectState` reads the last 50 non-empty lines and matches a fixed signal set. Fuzzy by design; loses to UI changes.

Before any of that, `ks list` and the TUI ask `Launcher.Alive`: a `stopped` record, or no tagged window matching `kitty_window_id`, is `stopped` without reading text. Full event tables and the classifier rules: `docs/hooks-and-state.md`.

## Build / run / test

```bash
make build      # ./ks  (stamps `ks version` with the short git SHA via -ldflags; fallback "dev")
make install    # ~/code/bin/ks — also strips macOS quarantine xattr + ad-hoc codesigns
make test       # go test -timeout 30s ./...
make fmt        # golines -m 100 --base-formatter=gofumpt -w .
make lint       # golangci-lint run ./...
```

Tests live next to the code. The heavier ones set `HOME` to a `t.TempDir()` so the session/state/config code reads from the tempdir; a few shell out to real `git init` / `git remote add` to exercise finder logic. Nothing drives kitty: `internal/kitty` tests inject a runner and assert the argument list, `internal/launcher` tests run against the in-memory instance in `fake_test.go` (window table, call sequence), `internal/instance` against a scripted pinger. `internal/cli/repo_test.go` is the pattern for capturing cobra output against a fake `HOME`.

Live check by hand, with the branch build and never `make install`: `./ks new -n smoke -d /tmp/smoke`. Then `kitty @ --to unix:$HOME/.config/ks/kitty.sock ls` should show the home tab plus a `smoke` tab, with the sidebar at `sidebar_width` columns and claude beside it. `./ks quit` ends the instance. Running it from inside an agent session is fine: the instance's environment is scrubbed. Check that `ls` shows no `CLAUDE*` keys in the claude window's `env`, and that `kitty @ ... action neighboring_window left` from the claude window lands on the sidebar.

**Adding a subcommand:** add `internal/cli/<name>.go` with a `var <name>Cmd` and an `init()` that calls `rootCmd.AddCommand(...)`. Put non-trivial logic in a new `internal/` package. Reach the instance through `ensureWiring` / `offlineWiring`, and session/state through `session.NewStore()` and `internal/state`, never the files directly. Add a test.

## Runtime prerequisites

- **kitty on the PATH.** `ks` starts its own instance with `-o allow_remote_control=yes` on the socket from `kitty_socket`; the user's `kitty.conf` needs no remote-control settings and `KITTY_LISTEN_ON` is irrelevant, since every call passes `--to`. The instance still reads `kitty.conf` for appearance. Verify with `kitty @ --to unix:$HOME/.config/ks/kitty.sock ls` while an instance runs. Tested with kitty 0.48.2.
- **`claude` on the caller's PATH.** Every launch path forwards `PATH` into both windows via `--env`, so a `claude` under `~/.local/bin` is found even though `kitty @ launch` runs with the instance's environment.

## Repo finder

`ks repo` (and the TUI `n` picker) walk the `dirs` from `config.yaml` with a 32-worker concurrent BFS, stopping at the first `.git` in any subtree, deduped by absolute path. Names come from parsing the `origin` URL in `.git/config` (SSH and HTTPS; last two path components for deep GitLab subgroups); no `origin` → fallback name `<parent>/<dir>` with empty host. Output modes: interactive fuzzy finder (default), `--list` (TSV), `--json`, `--toon` (token-efficient, for LLM consumers). The MCP/search/zoekt stack that once lived here now lives in [`csl`](https://github.com/mad01/code-search-local); `ks` kept `repo` only so the shell `repo()` helper keeps working.

## Dotfiles wiring & catalog

Installed via the dotfiles `kitty-session` recipe (`recipes/kitty-session/` — clones this repo, `make`-builds, installs `~/code/bin/ks`, symlinks `config.yaml`, registers the `ct` shell function = `ks tmp`). Ensure `~/code/bin` is on `$PATH`. Uninstalling the recipe wipes `~/.config/ks/sessions/`.

Catalogued via root `service-info.yaml`: System `kitty-session`, Component `ks`. Update it if the tool's shape changes; run `catalog validate .` before committing (names are globally unique across the catalog).

## Gotchas

- **Kitty IDs are per instance.** Never decide liveness or close a tab from a stored id alone; go through `Launcher.Alive` / the tagged-window snapshot (`findLive` in `internal/launcher/topology.go`).
- **The instance inherits the environment of whoever starts it.** `kitty.Start` drops `CLAUDE*`, `KS_*` and `KITTY_*` (keeping `KITTY_CONFIG_DIRECTORY`), and every launch unsets the agent-session markers in the child (`unsetInWindows` in `client.go`). Without that, a ks run from inside a Claude Code session made every claude it hosted a child session with transcript saving off. `PATH` is forwarded per window.
- **A new tab opens in the `fat` layout**, which ignores `--location=vsplit`; the topology switches it to `splits` first. Split sizes are fractions, so the sidebar is pinned by measuring `columns` and resizing, up to two passes.
- **`layout_action` acts on the tab's active window, not the `--match` one.** Right after the split that is claude, so `moveSidebarLeft` focuses the sidebar before `move_to_screen_edge left` and `splitClaude` hands focus back. `kitty @ ls` has no positions; verify placement with `action neighboring_window left` from the claude window, which must land on the sidebar.
- **Never pass `--title` to the claude window.** Claude's own OSC title is the state signal (and a future sidebar input).
- **`ks quit` ends every session's claude** (SIGHUP). Records stay `active` and come back on the next `ks`. A reopen uses `--resume <id>` with the record's own transcript, `--continue` when the directory has any transcript, and a bare `claude` otherwise. A session that never got a message comes back as a fresh conversation. Attach re-checks launched windows after two seconds and prints `ks: <name> exited right after launch` for any that vanished.
- **`layout` and `summary` in `config.yaml` are inert.** They still parse so old files load.
- **The `--agent` monitor costs real Haiku calls.** Leave it off unless hooks are not installed.
