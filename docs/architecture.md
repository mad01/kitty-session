# Architecture

Contributor-oriented tour. Covers the package graph, data flow for the two most important operations (create session, detect state), storage layout on disk, and how to add a new subcommand.

## Package graph

```
cmd/ks
  └── internal/cli              cobra subcommands
        ├── internal/tui        bubbletea TUI
        │     ├── internal/claude    state classifier + Claude projects reader
        │     ├── internal/kitty     kitty @ remote-control wrapper
        │     ├── internal/session   session struct + file store
        │     ├── internal/state     state file read/write
        │     ├── internal/summary   summary tab launcher
        │     └── internal/repo      { config, finder }
        ├── internal/kitty
        ├── internal/session
        ├── internal/state
        ├── internal/summary
        └── internal/repo/{config,finder}
```

`internal/cli` depends on almost everything. `internal/tui` is its second consumer. Each leaf package has a single responsibility and no dependencies on its peers except through `internal/cli` or `internal/tui` composing them.

### Leaf package responsibilities

| Package | Responsibility |
|---|---|
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
  "kitty_summary_window_id": 89
}
```

`kitty_shell_window_id` is only populated with `layout: tab` (the shell is a sibling kitty tab rather than a split pane). `kitty_summary_window_id` is only populated when the summary tab is enabled.

State files are even smaller — see [Hooks and state detection](hooks-and-state.md#state-file).

## Flow: creating a session

Triggered by `ks new -n foo -d /path` or the TUI's repo picker.

```
cli.runNew / tui.createSession
    └── config.Load()                        ~/.config/ks/config.yaml
    └── kitty.LaunchTab(dir, ...)            → new OS window, returns Claude window ID
    └── kitty.SetTabTitle(name)
    └── kitty.FindTabForWindow(windowID)     → tab ID
    └── session.New(name, dir, tabID, windowID)
    └── if layout == "tab":
          kitty.LaunchTabInWindow(claudeWinID, dir)     → shell window
        else:
          kitty.LaunchSplit(dir)                         → shell pane
    └── if summary enabled:
          summary.LaunchTab(...)             → haiku summary tab
    └── kitty.FocusWindow(claudeWinID)
    └── store.Save(sess)                     → ~/.config/ks/sessions/<name>.json
```

Environment variables passed to `claude` include `KS_SESSION_NAME=<name>` so the `ks _hook` handler knows which state file to write.

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
