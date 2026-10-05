# CLAUDE.md — kitty-session

`ks` is a session manager for the [kitty](https://sw.kovidgoyal.net/kitty/) terminal. It runs a kitty instance of its own. Every Claude Code session is one tab in it: the ks sidebar (a Bubble Tea agent list) on the left and claude on the right. The tab bar is hidden; the sidebar is the tab list. It tracks each session's state and brings every session back on attach. The user's own kitty is never touched. `README.md` is the front door; the `docs/` set is the deep dive. This file is the agent-facing working context: where code lives, how the on-disk model works, and the gotchas that bite.

Go module `github.com/mad01/kitty-session`, builds to a single `ks` binary. Needs Go 1.25+.

## Where things live

```
cmd/ks/main.go              entry point — calls internal/cli
internal/cli/               cobra subcommands (attach = bare ks, new, open, close, list, rename, quit, sidebar, tmp, repo, import, hooks, _hook, _sidebar-demo, version)
internal/sidebar/           Bubble Tea agent list, run as `ks sidebar`; every side effect behind sidebar.Backend
internal/hooks/             the ks matcher groups in ~/.claude/settings.json (install, uninstall, installed)
internal/herdr/             reader for herdr's session.json (Load, Agents, Running); `ks import` is its only caller
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

`internal/cli` depends on almost everything. `instance` and `launcher` are the two mid-layer packages: `instance` owns the kitty process, `launcher` everything inside it, including `launcher.SidebarBackend`, the production `sidebar.Backend`. `sidebar` never imports `launcher`; `launcher` imports `sidebar` for the interface and row types. Leaf packages have a single responsibility and don't import their peers. See `docs/architecture.md` for the package graph and the create-session, attach and detect-state flows.

**`internal/kitty` is the one exec boundary.** `kitty.New(socket)` returns a `Client`; each method wraps one `kitty @` subcommand and always passes `--to <socket>`. `Client.Start` is the only call that runs the kitty binary itself (`kitty --detach --listen-on ...` plus the `-o` overrides the topology depends on). Nothing else in the tree calls `exec.Command("kitty", ...)`. If you need a new kitty interaction, add a method to `client.go`. The repo finder also avoids subprocesses: it parses `.git/config` directly, never running `git`.

**Which wiring to use in a command.** `ensureWiring` (in `internal/cli/wiring.go`) starts the instance when it is down; `new`, `open`, `tmp` and attach use it. `offlineWiring` never starts it; `close`, `rename` and `list` use it so they work while the instance is down. Both hand back the store, the config (nil when the file is absent) and a `Launcher`.

## On-disk model

Everything `ks` writes lives under `~/.config/ks/`:

```
~/.config/ks/
├── config.yaml            user-authored (the only config; no per-repo, no env override for the path)
├── kitty.sock             the instance's remote-control socket (kitty_socket in config)
├── sessions/<name>.json   one per live session
├── sessions/trash/<name>.json   moved here by `ks close` (no --keep) and the sidebar's delete
└── state/<name>.json      written by hooks or the --agent monitor
```

A session file holds the kitty IDs (`kitty_tab_id`, `kitty_window_id` for claude, `kitty_sidebar_window_id`), the working dir, a created-at stamp, `status`, the Claude session id and transcript path from the last `SessionStart` hook, `focused_at`, and `viewed_at`. The session's own sidebar stamps `viewed_at` while its tab is active; a state-file `idle` newer than it shows as `done`. **Kitty IDs restart from 1 in every instance**, so after `ks quit` a stored id can point at another session's window. The launcher therefore tags both windows of a session with the kitty user variable `KS_SESSION_ID=<id>` (`launch --var`, read back as `user_vars` in `kitty @ ls`) and only trusts an id when the tag matches. A record with no `id` owns nothing. Records from older versions may carry `kitty_shell_window_id` / `kitty_summary_window_id`; they are cleared on the next reopen.

State files are `{"state": "...", "updated_at": "..."}` where state is `working` / `idle` / `input` / `waiting`. The one freshness threshold lives in `internal/state/file.go`: `freshness = 10s`, the age under which `ks list` trusts the file outright and the sidebar honours an `input`.

## State detection — three sources, in preference order

1. **Claude Code hooks** (preferred, most accurate). `ks hooks install` registers a hidden `ks _hook` handler for `PreToolUse`→`working`, `Stop`→`idle`, `Notification`→`input`, `SessionStart`→`waiting` (plus `SessionEnd` for the stopped status) in `~/.claude/settings.json`. The handler keys off `KS_SESSION_ID` / `KS_SESSION_NAME`, which the launcher exports into both windows of a session. Unset env → handler exits silently, so the hook is safe to leave installed globally.
2. **Background Haiku agent** (optional fallback). `ks sidebar --agent` runs a long-running `claude` with a tight allow-list that polls `kitty @ --to <socket> get-text` every 5s and writes state files; `ks --agent` passes the flag to the home sidebar when it starts the instance. Killed with its process group when that sidebar exits.
3. **Title glyph / terminal text** (always-on). The sidebar reads the claude window's title from the `ls` snapshot it already has: `internal/claude.ParseTitle` maps Claude's leading spinner glyph to `working` and `✳` to `idle`. `ks list` instead runs `internal/claude.DetectState` over the last 50 non-empty lines of `get-text`. Fuzzy by design; loses to UI changes.

Before any of that, both ask `Launcher.Alive` (or `findLive` on the shared snapshot): a `stopped` record, or no tagged window matching `kitty_window_id`, is `stopped` without reading anything. The sidebar's full rule set is `resolveState` in `internal/launcher/sidebar_backend.go` (table-tested): fresh (<10 s) state-file `input` then `working` win first, then the title glyph, then the `✳` idle title (which becomes `done` when the state file's `idle` is newer than the record's `viewed_at`), then a state-file fallback. A fresh `working` file outranks `✳` because `✳` doubles as a spinner frame. Full event tables and the classifier rules: `docs/hooks-and-state.md`; the sidebar's rules: `docs/tui.md`.

## Build / run / test

```bash
make build      # ./ks  (stamps `ks version` with the short git SHA via -ldflags; fallback "dev")
make install    # ~/code/bin/ks — also strips macOS quarantine xattr + ad-hoc codesigns
make test       # go test -timeout 30s ./...
make fmt        # golines -m 100 --base-formatter=gofumpt -w .
make lint       # golangci-lint run ./...
```

Tests live next to the code. The heavier ones set `HOME` to a `t.TempDir()` so the session/state/config code reads from the tempdir; a few shell out to real `git init` / `git remote add` to exercise finder logic. Nothing drives kitty: `internal/kitty` tests inject a runner and assert the argument list, `internal/launcher` tests run against the in-memory instance in `fake_test.go` (window table, call sequence), `internal/instance` against a scripted pinger. `internal/cli/repo_test.go` is the pattern for capturing cobra output against a fake `HOME`.

Live check by hand, with the branch build and never `make install`: `./ks new -n smoke -d /tmp/smoke`. Then `kitty @ --to unix:$HOME/.config/ks/kitty.sock ls` should show just a `smoke` tab (the home tab closes once a session tab exists and comes back when the last one closes), with the sidebar at `sidebar_width` columns and claude beside it. `get-text --match id:<sidebar window>` should print the framed list: an `agents … tab order` header, a `smoke` row, a `new … menu` footer. `send-text` `m` to that window opens the menu, `\x1b` closes it. `./ks quit` ends the instance. Claude's folder-trust dialog defaults to "No, exit"; send a down arrow (`\x1b[B`) before `\r` to accept. `ks _sidebar-demo` previews the sidebar on fake data without kitty. Running it from inside an agent session is fine: the instance's environment is scrubbed. Check that `ls` shows no Claude session-marker keys (`CLAUDECODE`, `CLAUDE_CODE_*`) in the claude window's `env`, and that `kitty @ ... action neighboring_window left` from the claude window lands on the sidebar.

**Adding a subcommand:** add `internal/cli/<name>.go` with a `var <name>Cmd` and an `init()` that calls `rootCmd.AddCommand(...)`. Put non-trivial logic in a new `internal/` package. Reach the instance through `ensureWiring` / `offlineWiring`, and session/state through `session.NewStore()` and `internal/state`, never the files directly. Add a test.

## Runtime prerequisites

- **kitty on the PATH.** `ks` starts its own instance with `-o allow_remote_control=yes` on the socket from `kitty_socket`; the user's `kitty.conf` needs no remote-control settings and `KITTY_LISTEN_ON` is irrelevant, since every call passes `--to`. The instance still reads `kitty.conf` for appearance. Verify with `kitty @ --to unix:$HOME/.config/ks/kitty.sock ls` while an instance runs. Tested with kitty 0.48.2.
- **`claude` on the caller's PATH.** Every launch path forwards `PATH` into both windows via `--env`, so a `claude` under `~/.local/bin` is found even though `kitty @ launch` runs with the instance's environment.

## Repo finder

`ks repo` (and the sidebar's `n` picker, through `SidebarBackend.Repos`) walk the `dirs` from `config.yaml` with a 32-worker concurrent BFS, stopping at the first `.git` in any subtree, deduped by absolute path. Names come from parsing the `origin` URL in `.git/config` (SSH and HTTPS; last two path components for deep GitLab subgroups); no `origin` → fallback name `<parent>/<dir>` with empty host. Output modes: interactive fuzzy finder (default), `--list` (TSV), `--json`, `--toon` (token-efficient, for LLM consumers). The MCP/search/zoekt stack that once lived here now lives in [`csl`](https://github.com/mad01/code-search-local); `ks` kept `repo` only so the shell `repo()` helper keeps working.

## Dotfiles wiring & catalog

Installed via the dotfiles `kitty-session` recipe (`recipes/kitty-session/` — clones this repo, `make`-builds, installs `~/code/bin/ks`, symlinks `config.yaml`, registers the `ct` shell function = `ks tmp`). Ensure `~/code/bin` is on `$PATH`. Uninstalling the recipe wipes `~/.config/ks/sessions/`.

Catalogued via root `service-info.yaml`: System `kitty-session`, Component `ks`. Update it if the tool's shape changes; run `catalog validate .` before committing (names are globally unique across the catalog).

## Gotchas

- **Kitty IDs are per instance.** Never decide liveness or close a tab from a stored id alone; go through `Launcher.Alive` / the tagged-window snapshot (`findLive` in `internal/launcher/topology.go`).
- **The instance inherits the environment of whoever starts it.** `kitty.Start` drops the Claude Code session markers (`claudeMarkers` in `client.go`) and every `KS_*` / `KITTY_*` except `KITTY_CONFIG_DIRECTORY`, and every launch unsets those same markers in the child (`unsetInWindows`). It does NOT drop all `CLAUDE*`: `CLAUDE_CONFIG_DIR`, the Bedrock/Vertex flags and auth tokens must survive. Without the marker scrub, a ks run from inside a Claude Code session made every claude it hosted a child session with transcript saving off. `PATH` is forwarded per window.
- **A new tab opens in the `fat` layout**, which ignores `--location=vsplit`; the topology switches it to `splits` first. Split sizes are fractions, so the sidebar is pinned by measuring `columns` and resizing, up to two passes.
- **Session tabs are built hidden.** Both launches pass `--keep-focus`, so a tab appears only once its split and width pin have settled, through the one `focus-window` in `Open` (skipped for `Request.Background`, which Attach uses for every resume before it focuses one session). From the sidebar, `--location=vsplit` already puts claude on the right. Never add a `layout_action` to the topology: `kitty @ action --match … layout_action` acts on the active window of the *visible* tab whatever `--match` names (probed 2026-10-05: an action aimed at a hidden tab moved the visible tab's window).
- **Never pass `--title` to the claude window.** Claude's own OSC title is the sidebar's state signal and its row title.
- **`--next-to` is ignored unless `--match` selects its tab.** `LaunchHSplit` passes both (`--match=id:<claude> --next-to=id:<claude>`); with `--next-to` alone kitty splits the active tab's active window instead. `LaunchVSplit` gets away with `--match` only because the sidebar is the tab's sole window at that point.
- **ks maps no keys in the instance.** Moving between the sidebar and claude is the user's own kitty window keys (`cmd+]` / `cmd+[` in the dotfiles config) plus `l`/`tab`/`q`/`enter` from the sidebar. `ctrl+b` and `ctrl+w` reach Claude (background a task, delete a word). Chords are opt-in: `kitty_overrides` entries like `map ctrl+b>s neighboring_window left` go to kitty as `-o`, which accepts `map` lines.
- **The home tab exists only while no session tab does.** `Open` retires it (`retireHome` in `topology.go`) once a session tab exists, except a home tab whose sidebar carries `KS_HOME_AGENT` (set on itself by `ks sidebar --agent`); `closeTabs` recreates it (`openHome`) before closing the last session tab, because the instance quits with its last window. A home tab is any tab with no `KS_SESSION_ID`-tagged window; never identify it by id.
- **The sidebar never exits on its own.** `ctrl+c` is swallowed; the only exits are the menu's `quit ks` (instance.Shutdown) and the instance going away. Run by hand with no instance it exits at once with `ks instance not running`. Inside the instance it waits for the socket (`instance.Await`, keyed on `KITTY_LISTEN_ON`), since kitty starts the home sidebar before the socket necessarily answers.
- **Sidebar frames are written inside synchronized-output markers** (DEC mode 2026, `syncOutput` in `internal/sidebar/output.go`). Recolouring the frame rewrites every line, and the macOS pty feeds kitty large writes in small chunks; the markers make kitty paint the whole frame at once instead of flickering through partial ones. Terminals without the mode ignore the markers.
- **`ks quit` ends every session's claude** (SIGHUP). Records stay `active` and come back on the next `ks`. A reopen uses `--resume <id>` with the record's own transcript, `--continue` when the directory has any transcript, and a bare `claude` otherwise. A session that never got a message comes back as a fresh conversation. Attach re-checks launched windows after two seconds and prints `ks: <name> exited right after launch` for any that vanished.
- **`layout` and `summary` in `config.yaml` are inert.** They still parse so old files load.
- **The `--agent` monitor costs real Haiku calls.** Leave it off unless hooks are not installed.
