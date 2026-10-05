# CLAUDE.md — kitty-session

`ks` is a session manager for the [kitty](https://sw.kovidgoyal.net/kitty/) terminal. It creates named kitty tabs that pair Claude Code with a shell, tracks each session's state, and lets you jump between them from a Bubble Tea TUI or from shell scripts. `README.md` is the front door; the nine-page `docs/` set is the deep dive. This file is the agent-facing working context: where code lives, how the on-disk model works, and the gotchas that bite.

Go module `github.com/mad01/kitty-session`, builds to a single `ks` binary. Needs Go 1.25+.

## Where things live

```
cmd/ks/main.go              entry point — calls internal/cli
internal/cli/               cobra subcommands (new, open, close, list, rename, repo, hooks, _hook, version, tmp, agent)
internal/tui/               Bubble Tea TUI (the no-subcommand path)
internal/launcher/          one launch path (Open) and one close path (Close) for new/open/tmp/close/TUI
internal/procinfo/          parent pid + comm lookup (darwin sysctl); the hook's nested-claude guard
internal/kitty/client.go    the ONLY place that shells out to `kitty @`
internal/session/           Session struct + Store (save/load/list/delete/rename/restore)
internal/state/file.go      state JSON read/write + freshness predicates
internal/claude/            terminal-text classifier (DetectState) + Claude projects reader (LatestPrompt)
internal/repo/config/       ~/.config/ks/config.yaml loader
internal/repo/finder/       concurrent BFS repo walker + remote-URL parser
internal/summary/           Haiku summary-tab launcher
```

`internal/cli` depends on almost everything; `internal/tui` is its second consumer. `internal/launcher` is the one mid-layer package (it composes `kitty`, `summary`, `session`, `config`); the other leaf packages have a single responsibility and don't import their peers. Anything that creates or reopens a session goes through `launcher.Open`. Don't call `kitty.LaunchTab` from a command. See `docs/architecture.md` for the package graph and data-flow diagrams (create-session, detect-state).

**`internal/kitty` is the one exec boundary.** One function there wraps each `kitty @` subcommand (`launch`, `ls`, `get-text`, `send-text`, `focus-tab`, `close-tab`, `set-tab-title`). Nothing else in the tree calls `exec.Command("kitty", ...)`. If you need a new kitty interaction, add a wrapper to `client.go` rather than shelling out elsewhere. The repo finder also avoids subprocesses: it parses `.git/config` directly, never running `git`.

## On-disk model

Everything `ks` writes lives under `~/.config/ks/`:

```
~/.config/ks/
├── config.yaml            user-authored (the only config; no per-repo, no env override for the path)
├── sessions/<name>.json   one per live session
├── sessions/trash/<name>.json   moved here by `ks close` (no --keep) and TUI delete
└── state/<name>.json      written by hooks or the --agent monitor
```

A session file holds the kitty IDs (`kitty_tab_id`, `kitty_window_id`, and optionally `kitty_shell_window_id` for `layout: tab` / `kitty_summary_window_id` when summary is on), the working dir and a created-at stamp. It also holds a stable random `id` (exported as `KS_SESSION_ID`; survives renames, `Store.FindByID`) and three lifecycle fields: `status` (`active` | `stopped`; absent in old files = active, see `session.IsActive`), `claude_session_id` and `claude_transcript_path` (both from the last `SessionStart` hook).

**Kitty IDs do not survive a kitty restart.** A record whose `status` is `stopped`, or whose tab is gone, shows `stopped` in the list. `ks open` treats a session as alive only when the tab *and* the claude window exist. Otherwise it closes leftover tabs and relaunches with `claude --resume <claude_session_id>` if the transcript file still exists, else `claude --continue`. The launcher saves the record before the first kitty call and writes only kitty IDs back afterwards, so the hook's `SessionStart` update is never clobbered. `status: stopped` is written by `launcher.Close` (behind `ks close --keep` and the TUI close action) and by the `SessionEnd` hook (reasons `prompt_input_exit`/`logout` only). `Store.Save` is atomic (temp file + rename). Session files are safe to hand-edit when an ID goes stale.

State files are `{"state": "...", "updated_at": "..."}` where state is `working` / `idle` / `input` / `waiting`. Two freshness thresholds live in `internal/state/file.go`: `freshness = 10s` (trusted outright) and `IsRecentlyWorking = 5min` (a stale `working` is still honored unless terminal text clearly says idle or input).

## State detection — three sources, in preference order

1. **Claude Code hooks** (preferred, most accurate). `ks hooks install` registers a hidden `ks _hook` handler for `PreToolUse`→`working`, `Stop`→`idle`, `Notification`→`input`, `SessionStart`→`waiting`, plus `SessionEnd` (matcher `prompt_input_exit|logout`; no state; marks `status: stopped` and removes the state file) in `~/.claude/settings.json`. `SessionStart` also records `claude_session_id` and `claude_transcript_path` on the session file. The handler resolves the record by `KS_SESSION_ID` first, then `KS_SESSION_NAME`; both are exported by every launch path. Unset name → handler exits silently, so the hook is safe to leave installed globally. Before writing anything it checks (via `internal/procinfo`) that the firing Claude is a direct child of `kitty`. A `claude -p` run inside the session inherits the env but is not the session's Claude, and must not touch its state. Events with an `agent_id` (subagents) update state but never the record. Record errors go to stderr as `ks _hook: ...`; the hook always exits 0. Hook payloads are flat JSON: `hook_event_name`, `session_id`, `transcript_path`, `cwd`, plus `tool_name` / `notification_type` / `source` / `reason` / `agent_id` per event.
2. **Background Haiku agent** (`ks --agent`, optional fallback). A long-running `claude` with a tight allow-list that polls `kitty @ get-text` every 5s and writes state files. Killed with its process group when the TUI exits.
3. **Terminal-text heuristic** (always-on). `internal/claude.DetectState` reads the last 50 non-empty lines and matches a fixed signal set. Fuzzy by design; loses to UI changes.

`ks list` and the TUI both compute state through this same chain. Full event tables and the classifier rules: `docs/hooks-and-state.md`.

## Build / run / test

```bash
make build      # ./ks  (stamps `ks version` with the short git SHA via -ldflags; fallback "dev")
make install    # ~/code/bin/ks — also strips macOS quarantine xattr + ad-hoc codesigns
make test       # go test -timeout 30s ./...
make fmt        # gofmt -w .
make lint       # golangci-lint run ./...
```

Tests live next to the code. The heavier ones set `HOME` to a `t.TempDir()` so the session/state/config code reads from the tempdir; a few shell out to real `git init` / `git remote add` to exercise finder logic. There are no integration tests driving kitty — `internal/kitty` is a thin wrapper, exercised by hand. `internal/cli/repo_test.go` is the pattern for capturing cobra output against a fake `HOME`.

**Adding a subcommand:** add `internal/cli/<name>.go` with a `var <name>Cmd` and an `init()` that calls `rootCmd.AddCommand(...)`; put non-trivial logic in a new `internal/` package; reach session/state through `session.NewStore()` and `internal/state`, never the files directly; add a test.

## Runtime prerequisites

- **kitty with remote control enabled** — `allow_remote_control yes` + a `listen_on` socket in `kitty.conf`, then restart kitty. `kitty @` finds the socket through `KITTY_LISTEN_ON`, which kitty exports into its own terminals; on this machine it is `unix:~/.config/kitty/default`. Without it every launch/focus call fails. Verify with `kitty @ ls` (should print JSON).
- **`claude` on the PATH kitty sees.** `kitty @ launch` uses kitty's environment, not the current shell's. The launcher forwards the caller's `PATH` via `--env PATH=...` on every launch path, so `claude` only has to be reachable from the shell that runs `ks`.

## Repo finder

`ks repo` (and the TUI `n` picker) walk the `dirs` from `config.yaml` with a 32-worker concurrent BFS, stopping at the first `.git` in any subtree, deduped by absolute path. Names come from parsing the `origin` URL in `.git/config` (SSH and HTTPS; last two path components for deep GitLab subgroups); no `origin` → fallback name `<parent>/<dir>` with empty host. Output modes: interactive fuzzy finder (default), `--list` (TSV), `--json`, `--toon` (token-efficient, for LLM consumers). The MCP/search/zoekt stack that once lived here now lives in [`csl`](https://github.com/mad01/code-search-local); `ks` kept `repo` only so the shell `repo()` helper keeps working.

## Dotfiles wiring & catalog

Installed via the dotfiles `kitty-session` recipe (`recipes/kitty-session/` — clones this repo, `make`-builds, installs `~/code/bin/ks`, symlinks `config.yaml`, registers the `ct` shell function = `ks tmp`). Ensure `~/code/bin` is on `$PATH`. Uninstalling the recipe wipes `~/.config/ks/sessions/`.

Catalogued via root `service-info.yaml`: System `kitty-session`, Component `ks`. Update it if the tool's shape changes; run `catalog validate .` before committing (names are globally unique across the catalog).

## Gotchas

- **Kitty IDs are ephemeral.** Anything storing a `kitty_*` ID goes stale on a kitty restart. Recreate via `ks open`, don't trust a stored ID across kitty process boundaries.
- **`summary: true` requires `layout: tab`.** Under `layout: split` there's no room for a third pane, so the flag is silently ignored.
- **`config.yaml` is required only for `ks repo` and the TUI picker.** `ks new`/`ks open` work without it (default `split` layout, no summary).
- **The summary tab and `--agent` cost real Haiku calls.** The cadence is conservative, but long-running sessions add up; leave `summary: false` if cost matters.
