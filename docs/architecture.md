# Architecture

Contributor-oriented tour. Covers the package graph, data flow for the two most important operations (create session, detect state), storage layout on disk, and how to add a new subcommand.

## Package graph

```
cmd/ks
  └── internal/cli              cobra subcommands
        ├── internal/tui        bubbletea TUI
        │     ├── internal/launcher  create/reopen a session in kitty
        │     ├── internal/claude    state classifier + Claude projects reader
        │     ├── internal/kitty     kitty @ remote-control wrapper
        │     ├── internal/session   session struct + file store
        │     ├── internal/state     state file read/write
        │     └── internal/repo      { config, finder }
        ├── internal/launcher
        │     ├── internal/kitty
        │     ├── internal/session
        │     ├── internal/summary   summary tab launcher
        │     └── internal/repo/config
        ├── internal/kitty
        ├── internal/session
        ├── internal/state
        └── internal/repo/{config,finder}
```

`internal/cli` depends on almost everything. `internal/tui` is its second consumer. `internal/launcher` is the one mid-layer package: it composes `kitty`, `summary`, `session`, and `config` so that `cli` and `tui` share a single launch path. Every other leaf package has a single responsibility and no dependencies on its peers.

### Leaf package responsibilities

| Package | Responsibility |
|---|---|
| `internal/launcher` | `Open(store, cfg, Request)`: build the claude command line (`claude`, `claude --resume <id>`, or `claude --continue`), lay out the kitty windows, save the record. The layout lives in one function (`launchTopology`) behind a small backend interface so tests run without kitty. |
| `internal/kitty` | Shell out to `kitty @` subcommands; parse `@ ls` JSON. No knowledge of sessions or Claude. |
| `internal/session` | `Session` struct and `Store` (save/load/list/delete/rename/restore) backed by `~/.config/ks/sessions/`. |
| `internal/state` | JSON state files under `~/.config/ks/state/`. Freshness predicates. |
| `internal/claude` | Terminal-text classifier (`DetectState`) and `LatestPrompt` (reads `~/.claude/projects/*/sessions-index.json`). |
| `internal/repo/config` | `~/.config/ks/config.yaml` loader; `Layout` / `Summary` / `TmpDir` accessors. |
| `internal/repo/finder` | Concurrent BFS repo walker; remote URL parser for name/host. |
| `internal/summary` | Launches the Haiku summary tab with its system prompt and allow-listed tools. |

## Storage layout

Everything `ks` writes lives under `~/.config/ks/`:

```
~/.config/ks/
├── config.yaml            # user-authored
├── sessions/
│   ├── <name>.json        # one per live session
│   └── trash/
│       └── <name>.json    # moved here by `ks close` (no --keep) and TUI delete
└── state/
    └── <name>.json        # written by hooks or the --agent monitor
```

Session files are small JSON:

```json
{
  "name": "kitty-session-main",
  "dir": "/Users/you/code/src/github.com/mad01/kitty-session",
  "created_at": "2026-04-16T10:15:00Z",
  "kitty_tab_id": 42,
  "kitty_window_id": 87,
  "kitty_shell_window_id": 88,
  "kitty_summary_window_id": 89,
  "status": "active",
  "claude_session_id": "6f1c2a4e-3b7d-4c0e-9a51-2f8e7d6c5b4a"
}
```

`kitty_shell_window_id` is only populated with `layout: tab` (the shell is a sibling kitty tab rather than a split pane). `kitty_summary_window_id` is only populated when the summary tab is enabled.

The `kitty_*` IDs are ephemeral and go stale when kitty restarts. `status` and `claude_session_id` are not: `status` is `active` or `stopped` (absent in files from older versions, which read as `active`), and `claude_session_id` is the ID Claude Code reported on its last `SessionStart` hook. Together they let `ks open` bring back a conversation with `claude --resume <id>`. See [Hooks and state detection](hooks-and-state.md#session-id-and-status) for who writes them. `Store.Save` writes through a temp file and rename, so readers never see a partial record.

State files are even smaller — see [Hooks and state detection](hooks-and-state.md#state-file).

## Flow: creating a session

Triggered by `ks new -n foo -d /path`, `ks tmp`, `ks open <stopped>`, or the TUI. All of them call `launcher.Open`; only the `Request` differs (`ResumeNone` for a new session, `ResumeStored` to focus or recreate an existing one).

```
cli.runNew / cli.runTmp / cli.runOpen / tui.createSession / tui.openSession
    └── config.Load()                        ~/.config/ks/config.yaml
    └── launcher.Open(store, cfg, Request{Name, Dir, Resume})
          ├── target: session.New(...) or store.Load(name)
          ├── ResumeStored and tab alive → focus it, return (nothing saved)
          ├── claudeArgs: --env PATH, --env KS_SESSION_NAME, -- claude [--resume <id> | --continue]
          ├── launchTopology(plan)
          │     ├── kitty.LaunchTab(dir, claudeArgs...)     → new OS window, Claude window ID
          │     ├── kitty.SetTabTitle(name)
          │     ├── kitty.FindTabForWindow(windowID)        → tab ID
          │     ├── layout tab:  kitty.LaunchTabInWindow    → shell window
          │     │   layout split: kitty.LaunchSplit         → shell pane
          │     ├── summary enabled: summary.LaunchTab      → haiku tab (failure = warning)
          │     └── kitty.FocusWindow(claudeWinID)          (failure = warning)
          ├── copy IDs onto the record, status = active
          └── store.Save(sess)                              → ~/.config/ks/sessions/<name>.json
```

`claude` is started with `--resume <id>` when the record has a `claude_session_id`, `--continue` when it is a reopen without one, and bare for a new session. `PATH` is forwarded because `kitty @ launch` runs with kitty's environment, not the caller's. `KS_SESSION_NAME=<name>` lets the `ks _hook` handler find the state file and record. Callers print the launcher's warnings (summary tab, focus) themselves; the TUI drops them.

## Flow: detecting a session's state

Triggered by every TUI poll (every 3 seconds) and by every `ks list` invocation.

```
detectSessionState(sess):
    if !kitty.TabExists(sess.KittyTabID):
        return StateStopped
    state.Read(sess.Name):
        if fresh (<10s):
            return parsed
        if state was "working" and <5min old:
            look at terminal; prefer idle/input if they show; else trust "working"
    return DetectState(kitty.GetText(sess.KittyWindowID))
```

See [Hooks and state detection](hooks-and-state.md) for the textual rules inside `DetectState`.

## Kitty protocol usage

`internal/kitty/client.go` is the only place that shells out to `kitty`. Every function wraps one subcommand:

| Function | Wraps |
|---|---|
| `LaunchTab(dir, args...)` | `kitty @ launch --type=os-window --cwd=<dir> -- <args>` |
| `LaunchTabInWindow(winID, dir, args...)` | `kitty @ launch --type=tab --match=id:<winID> --cwd=<dir> -- <args>` |
| `LaunchSplit(dir, args...)` | `kitty @ launch --type=window --location=hsplit --bias=30 --cwd=<dir> -- <args>` |
| `SetTabTitle(title)` | `kitty @ set-tab-title <title>` |
| `SetTabTitleForWindow(title, winID)` | `kitty @ set-tab-title --match=id:<winID> <title>` |
| `FocusTab(tabID)` | `kitty @ focus-tab --match=id:<tabID>` |
| `FocusWindow(winID)` | `kitty @ focus-window --match=id:<winID>` |
| `CloseTab(tabID)` | `kitty @ close-tab --match=id:<tabID>` |
| `CloseTabForWindow(winID)` | `kitty @ close-tab --match=id:<winID>` |
| `ListTabs` / `ListTabsDetailed` | `kitty @ ls` (JSON out) |
| `GetText(winID)` | `kitty @ get-text --match=id:<winID>` |
| `SendText(winID, text)` | `kitty @ send-text --match=id:<winID> <text>` |
| `FindTabForWindow(winID)` | Walks `ListTabsDetailed` output |
| `FirstWindowInTab(tabID)` | Walks `ListTabsDetailed` output |
| `TabExists(tabID)` | Walks `ListTabs` output |

Nothing else in the codebase calls `exec.Command("kitty", ...)`.

## Adding a subcommand

1. Add `internal/cli/<name>.go`. Declare a `var <name>Cmd = &cobra.Command{...}` and an `init()` that calls `rootCmd.AddCommand(<name>Cmd)`.
2. Put the business logic in a new package under `internal/` if it's non-trivial, and call into it from the command's `RunE`.
3. If the command needs state from the session store or state files, use `session.NewStore()` and the `internal/state` package — do not reach into the files yourself.
4. Add tests. The existing `internal/cli/repo_test.go` shows the pattern for capturing cobra output and exercising commands against a fake `HOME`.

## Testing

```bash
make test    # go test -timeout 30s ./...
```

Tests live next to the code they exercise. Heavier ones create a fake `HOME` with `t.TempDir()` and `os.Setenv("HOME", tmp)` so the session/state/config code reads from the tempdir. A handful of tests shell out to real `git` (`git init`, `git remote add`) — the assertion is on the finder/repo logic, not on `git` itself.

There are no integration tests that drive kitty. `internal/kitty` is intentionally a thin wrapper with no logic worth testing in isolation; behavior is exercised by hand.

## Build

```bash
make build      # ./ks
make install    # ~/code/bin/ks (macOS: also ad-hoc signs)
```

`make build` passes `-ldflags "-X github.com/mad01/kitty-session/internal/cli.Version=<git-sha>"` so `ks version` prints the short commit. The fallback is `dev`.

## Formatting and linting

```bash
make fmt    # gofmt -w .
make lint   # golangci-lint run ./...
```

No custom linters or build tags are in use.
