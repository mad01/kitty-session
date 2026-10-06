# Architecture

Contributor-oriented tour. Covers the package graph, data flow for the three most important operations (create session, attach, detect state), storage layout on disk, and how to add a new subcommand.

## Package graph

```
cmd/ks
  └── internal/cli              cobra subcommands; bare ks is attach
        ├── internal/sidebar        bubbletea agent list, run as `ks sidebar`; side effects behind Backend
        ├── internal/launcher       open/close/rename/attach sessions; SidebarBackend implements sidebar.Backend
        │     ├── internal/sidebar   the Backend interface and row types
        │     ├── internal/instance  Shutdown, for the menu's quit
        │     ├── internal/kitty
        │     ├── internal/session
        │     ├── internal/state
        │     ├── internal/claude    transcript path for --resume; ParseTitle for the row state
        │     ├── internal/hooks     hook status for the menu
        │     └── internal/repo      { config, finder }
        ├── internal/instance       start/connect/shut down the ks kitty instance
        │     ├── internal/kitty
        │     └── internal/repo/config
        ├── internal/hooks          ks hook groups in ~/.claude/settings.json
        ├── internal/herdr          herdr's session.json: parse, flatten to claude agents, daemon check
        ├── internal/procinfo       process parent/comm lookup (hook's nested-claude guard)
        ├── internal/kitty
        ├── internal/session
        ├── internal/state
        └── internal/repo/{config,finder}
```

`internal/cli` depends on almost everything. Two mid-layer packages sit between it and the leaves: `internal/instance` owns the kitty process (is it up, start it, close it), `internal/launcher` owns everything that happens inside it (tabs, windows, records). `internal/sidebar` is the UI and knows nothing of kitty or the store: it drives a `Backend` interface of 16 methods, which `launcher.SidebarBackend` implements for production and `sidebar.NewDemoBackend` for `ks _sidebar-demo`. The sidebar package never imports the launcher; the launcher imports the sidebar for its types. Every other leaf package has a single responsibility and no dependencies on its peers.

### Leaf package responsibilities

| Package | Responsibility |
|---|---|
| `internal/instance` | `Ensure(cfg, opts)` pings the configured socket. When nothing answers it removes the stale socket file, runs `kitty --detach --listen-on <socket> -o ...` with `ks sidebar` as the first window, and polls until the socket answers (100 ms, up to 10 s). `Connect` for commands that must not start it; `Client` for commands that must work while it is down; `Shutdown` closes every window. |
| `internal/launcher` | `Launcher.Open(Request)`: reject a taken name (`ErrExists`), save the record, build the claude command line (`claude --resume <id>`, `claude --continue`, or a bare `claude` when the directory has no transcript), lay out the tab, write the kitty IDs back. `Close(sess, keep)`, `Rename(old, new)`, `Move(name, where)` (focus the tab, step it with `move_tab_forward`/`move_tab_backward`, refocus, then rank every open session's `position` by the new tab order), `Attach()`, `Alive(sess)`. `SuggestName(dir)` (base name plus git branch) and `ScratchDir(base)`. `SidebarBackend` maps the sidebar's actions onto all of that and resolves each row's state (below). Layout and teardown sit behind a small backend interface so tests run without kitty. |
| `internal/sidebar` | The agent list: model, view, key and mouse handling, menu, picker, demo backend. `Run(Options{Session, Width, Backend})`. Polls `Backend.List` every 3 s. |
| `internal/hooks` | `Install`, `Uninstall` and `Installed` for the five ks matcher groups in `~/.claude/settings.json`; other tools' entries are kept. |
| `internal/herdr` | `Load(path)` parses herdr's `session.json` (format version 3 only), `Agents()` flattens workspaces, tabs and panes into the claude panes ks can import plus the skipped ones with a reason, `Running(dir)` dials `herdr.sock` to tell whether herdr still owns them. Stdlib only; `ks import` does the wiring. |
| `internal/procinfo` | `ParentOf(pid)` and `CommOf(pid)` via the darwin `kern.proc.pid` sysctl; `ErrUnsupported` elsewhere. Used by the hook to tell the Claude kitty launched from one nested inside the session. |
| `internal/kitty` | `Client`: every `kitty @` call against one `--to` socket; `Start` runs the kitty binary itself. Parses `@ ls` JSON into `Window` values. No knowledge of sessions beyond `SessionVar`, the user variable that tags ks windows. |
| `internal/session` | `Session` struct and `Store` (save/load/list/delete/rename/restore) backed by `~/.config/ks/sessions/`. |
| `internal/state` | JSON state files under `~/.config/ks/state/`. Freshness predicates. |
| `internal/claude` | Terminal-text classifier (`DetectState`), tab-title glyph parser (`ParseTitle`), and the transcript path helpers behind the `--resume` decision. |
| `internal/repo/config` | `~/.config/ks/config.yaml` loader; `Socket`, `EffectiveSidebarWidth`, `Overrides`, `EffectiveTmpDir` accessors. |
| `internal/repo/finder` | Concurrent BFS repo walker; remote URL parser for name/host. |

## Storage layout

Everything `ks` writes lives under `~/.config/ks/`:

```
~/.config/ks/
├── config.yaml            # user-authored
├── kitty.sock             # the instance's remote-control socket (kitty_socket in config)
├── sessions/
│   ├── <name>.json        # one per live session
│   └── trash/
│       └── <name>.json    # moved here by `ks close` (no --keep) and the sidebar's delete
└── state/
    └── <name>.json        # written by hooks or the --agent monitor
```

Session files are small JSON:

```json
{
  "id": "9f2c7b1e4d6a8c0f9f2c7b1e4d6a8c0f",
  "name": "kitty-session-main",
  "dir": "/Users/you/code/src/github.com/mad01/kitty-session",
  "created_at": "2026-04-16T10:15:00Z",
  "kitty_tab_id": 2,
  "kitty_window_id": 3,
  "kitty_sidebar_window_id": 2,
  "status": "active",
  "claude_session_id": "6f1c2a4e-3b7d-4c0e-9a51-2f8e7d6c5b4a",
  "claude_transcript_path": "/Users/you/.claude/projects/-Users-you-code-src-github-com-mad01-kitty-session/6f1c2a4e-3b7d-4c0e-9a51-2f8e7d6c5b4a.jsonl",
  "focused_at": "2026-10-05T19:35:51Z",
  "viewed_at": "2026-10-05T19:36:02Z"
}
```

The `kitty_*` IDs are ephemeral. Kitty numbers tabs and windows from 1 in every instance, so they go stale when the instance restarts, and a stored id can then point at another session's window. That is why the launcher never trusts an id alone. Every window it launches carries the kitty user variable `KS_SESSION_ID=<id>` (`kitty @ launch --var`), visible as `user_vars` in `kitty @ ls`. A window counts as the session's only when the tag matches.

`id` is random and stable across renames (also exported as the `KS_SESSION_ID` environment variable, found with `Store.FindByID`). `status` is `active` or `stopped` (absent in files from older versions, which read as `active`). `claude_session_id` and `claude_transcript_path` are what Claude Code reported on its last `SessionStart` hook; together they let a reopen bring back a conversation with `claude --resume <id>` while the transcript file still exists.

`focused_at` is stamped whenever the launcher creates or focuses the session; attach brings the newest one to the front. `viewed_at` is stamped by the session's own sidebar while its tab is the active one (at most every 10 s); a state file `idle` newer than it shows the row as `done`. `position` is the session's rank among the sessions with a tab, counted from 1: written by `ks move`, and from then on refreshed by every tab launch, so a reopened session takes the bottom rank. Absent until a move; attach opens ranked sessions first, by rank, then the rest by creation. Files written by older `ks` versions may still carry `kitty_shell_window_id` and `kitty_summary_window_id`; the launcher clears them on the next reopen. `Store.Save` writes through a temp file and rename, so readers never see a partial record.

State files are even smaller, see [Hooks and state detection](hooks-and-state.md#state-file).

## Flow: creating a session

Triggered by `ks new -n foo -d /path`, `ks tmp`, `ks open <stopped>`, or the sidebar. All of them call `Launcher.Open`; only the `Request` differs (`ResumeNone` for a new session, `ResumeStored` to focus or recreate an existing one).

```
cli.runNew / cli.runTmp / cli.runOpen / SidebarBackend.New / SidebarBackend.Focus
    └── instance.Ensure(cfg)                 start the instance if its socket does not answer
    └── launcher.Open(Request{Name, Dir, Resume})
          ├── target: ResumeNone → ErrExists if the name is taken, else session.New(...)
          │           ResumeStored → store.Load(name), assigning an id if the record has none
          ├── ResumeStored: one kitty @ ls snapshot, windows tagged with the session id
          │     ├── claude window alive → FocusWindow, stamp focused_at, return
          │     ├── only the sidebar alive → relaunch claude beside it (below, from the vsplit on)
          │     └── otherwise → close every tab holding a tagged window
          ├── store.Save(sess) with status = active   (before kitty, so SessionStart finds it)
          ├── env for both windows: PATH, KS_SESSION_NAME, KS_SESSION_ID; var KS_SESSION_ID
          ├── launchTopology
          │     ├── anchor = first window in the instance
          │     ├── LaunchTab(anchor, dir, `ks sidebar --session-id <id>`)  → sidebar window, keep-focus: the tab stays hidden
          │     ├── GotoLayout(sidebar, splits)       a new tab opens in `fat`, which ignores vsplit
          │     ├── SetTabTitleForWindow(name, sidebar)
          │     ├── measure the sidebar: it spans the tab, so its columns are the tab width
          │     ├── LaunchVSplit(sidebar, bias)       bias = (width - sidebar_width) / width; keep-focus, claude lands on the right
          │     │     -- claude [--resume <id> if its transcript exists | --continue if the dir has any transcript | bare]
          │     └── pin: ResizeWindow(sidebar, horizontal, sidebar_width - columns), twice at most
          ├── store.Load(name), copy the kitty IDs and focused_at, store.Save
          └── FocusWindow(claude)   the one visible switch (failure = warning); skipped for Request.Background, which attach uses
```

The claude window never gets `--title`: Claude Code sets the window title itself through OSC, and that title (`◐ ...` while working, `✳ ...` idle) is a state signal. `PATH` is forwarded because `kitty @ launch` runs with the instance's environment, not the caller's. Every launch also passes `--env NAME` without a value for the agent-session markers (`CLAUDECODE`, `CLAUDE_CODE_CHILD_SESSION`, `CLAUDE_CODE_SESSION_ID`, `CLAUDE_PID`, `CLAUDE_CODE_ENTRYPOINT`), which unsets them in the child; `Client.Start` already drops the Claude Code session markers (`CLAUDECODE`, `CLAUDE_CODE_CHILD_SESSION`, `CLAUDE_CODE_SESSION_ID`, `CLAUDE_PID`, `CLAUDE_CODE_ENTRYPOINT`, `CLAUDE_CODE_SESSION_ATTENDED`, `CLAUDE_CODE_MESSAGING_SOCKET`, `CLAUDE_CODE_MESSAGING_TOKEN`, `CLAUDE_EFFORT`) and every `KS_*` and `KITTY_*` variable except `KITTY_CONFIG_DIRECTORY` from the instance's own environment, leaving the user's `CLAUDE_CONFIG_DIR`, Bedrock and Vertex flags and auth tokens in place. `KS_SESSION_NAME` and `KS_SESSION_ID` let the `ks _hook` handler find the record and its state file. The record is reloaded before the final save because the `SessionStart` hook may already have written `claude_session_id` while claude was starting. Callers print the launcher's warnings (geometry, focus, leftover tabs) themselves; the sidebar backend drops them, since the UI has one status line and the action itself succeeded.

Closing is the mirror image. `ks close` and the sidebar's close/delete actions call `Launcher.Close(sess, keep)`. It removes the state file, closes every tab holding a window tagged with the session id, and then marks the record `stopped` (keep) or moves it to `sessions/trash/`. An instance that cannot be reached is a warning; the record is handled regardless.

## Flow: attach

Bare `ks`.

```
cli.runAttach
    └── instance.Ensure(cfg)                 start the instance if needed
    └── launcher.Attach()
          ├── store.List()                   sorted for resume: position (ks move) first, then creation
          ├── stopped records → counted, untouched
          ├── target = the active record with the newest focused_at (the first in that order if none)
          ├── for each active record: Alive? → counted as running
          │                           else  → Open(ResumeStored), 100 ms apart
          ├── wait 2 s, re-check every launched claude window: gone again → Exited, else Resumed
          ├── Open(target, ResumeStored)     now alive, so this focuses and stamps focused_at
          │   (no active record → focus the home tab's sidebar)
          └── print `ks: N resumed, N already running, N stopped`
```

`ks quit` is `instance.Shutdown`: `kitty @ close-window --match all`, which ends the instance because it runs with `macos_quit_when_last_window_closed=yes`. Records stay `active`, so the next attach brings them all back.

## Flow: detecting a session's state

The sidebar polls `SidebarBackend.List` every 3 seconds. One `store.List()` and one `kitty @ ls` snapshot serve every row; windows are matched to records by the `KS_SESSION_ID` tag, never by stored id alone.

```
SidebarBackend.List():
    sessions = store.List(); all = kitty.Windows()
    for sess:
        lv = findLive(all, sess)                       # claude/sidebar windows tagged with sess.ID
        if sess is the own session and lv's tab is_active:
            stamp viewed_at (reload, save), at most every 10 s
        resolveState(record status, claude window present?, its title, state file, viewed_at):
            !IsActive or no claude window             → stopped
            state file input, < 10 s old              → input
            state file working, < 10 s old            → working   (a fresh working file outranks a ✳ title)
            ParseTitle(title) == working              → working
            ParseTitle(title) == idle                 → done if state file idle and updated_at > viewed_at, else idle
            no glyph: state file working / input      → working / input
            otherwise                                 → idle
        Title = title minus glyph, or ~-shortened dir; Tab = position of the session's tab in kitty's order
```

`ks list` has its own, older chain: `stopped` when the record is stopped or `Launcher.Alive` says no, a fresh state file's value if there is one, else `DetectState(kitty.GetText(...))` on the pane text. It pings the socket once first; when nothing answers every active session prints `stopped` and a trailing `ks instance not running` line. See [Hooks and state detection](hooks-and-state.md) for the textual rules inside `DetectState`.

## Kitty protocol usage

`internal/kitty/client.go` is the only place that runs `kitty`. `Client.Start` runs the binary itself; every other method is one `kitty @ --to <socket> <subcommand>`:

| Method | Wraps |
|---|---|
| `Start(opts)` | `kitty --detach --listen-on <socket> -o allow_remote_control=yes -o tab_bar_style=hidden -o window_border_width=0 -o window_margin_width=0 -o window_padding_width=3 -o macos_quit_when_last_window_closed=yes [-o <override>...] --title ks -- <command>`, with the caller's environment minus the Claude Code session markers and every `KS_*` / `KITTY_*` (keeping `KITTY_CONFIG_DIRECTORY` and the user's other `CLAUDE_*` configuration). No config file is written; a user's `kitty_overrides` may carry `map` lines, which kitty accepts through `-o`. Started with a 5 s deadline and closed stdin so a non-detaching kitty cannot block ks. |
| `Ping()` | `ls`, 2 s timeout |
| `Windows()` | `ls`, parsed into `Window{ID, TabID, TabTitle, TabActive, Title, Columns, SessionID}` |
| `AnyWindow`, `TabExists`, `WindowExists`, `FindTabForWindow`, `WindowColumns`, `WindowTitle` | Walk one `Windows()` snapshot |
| `LaunchTab(Launch)` | `launch --type=tab --match=id:<win> [--keep-focus] --cwd=<dir> --env <marker>... --env K=V... --var K=V... -- <command>` (a bare `--env NAME` unsets) |
| `LaunchVSplit(Launch)` | `launch --type=window --location=vsplit --bias=<n> --match=id:<win> [--keep-focus] --cwd=<dir> --env ... --var ... -- <command>` |
| `LaunchHSplit(Launch)` | `launch --type=window --location=hsplit --bias=<n> --match=id:<win> --next-to=id:<win> --cwd=<dir> ...`; `--next-to` names the window to split (kitty otherwise splits the tab's active window, the sidebar), `--match` picks the tab (without it kitty ignores `--next-to`) |
| `GotoLayout(win, layout)` | `goto-layout --match=id:<win> <layout>` |
| `ResizeWindow(win, axis, n)` | `resize-window --match=id:<win> --axis=<axis> --increment=<n>` |
| `FocusWindow(win)` | `focus-window --match=id:<win>` |
| `SetTabTitleForWindow(title, win)` | `set-tab-title --match=id:<win> <title>` |
| `CloseTab(tab)` | `close-tab --match=id:<tab>` |
| `CloseWindow(win)` | `close-window --match=id:<win>` |
| `CloseAll()` | `close-window --match=all` |
| `GetText(win)` | `get-text --match=id:<win>` |

The launcher only needs `Windows`, the three launches, `GotoLayout`, `ResizeWindow`, `SetTabTitleForWindow`, `FocusWindow` and `CloseTab`; that is its backend interface, and the test fake implements it with an in-memory window table.

## Adding a subcommand

1. Add `internal/cli/<name>.go`. Declare a `var <name>Cmd = &cobra.Command{...}` and an `init()` that calls `rootCmd.AddCommand(<name>Cmd)`.
2. Put the business logic in a new package under `internal/` if it's non-trivial, and call into it from the command's `RunE`.
3. Reach the instance through `ensureWiring` (starts it when needed) or `offlineWiring` (never starts it) in `internal/cli/wiring.go`; they hand back the store, the config and a `Launcher`. Use `session.NewStore()` and the `internal/state` package for records and state, never the files directly.
4. Add tests. The existing `internal/cli/repo_test.go` shows the pattern for capturing cobra output and exercising commands against a fake `HOME`; `internal/launcher/fake_test.go` is the in-memory kitty.

## Testing

```bash
make test    # go test -timeout 30s ./...
```

Tests live next to the code they exercise. Heavier ones create a fake `HOME` with `t.TempDir()` and `t.Setenv("HOME", tmp)` so the session/state/config code reads from the tempdir. A handful of tests shell out to real `git` (`git init`, `git remote add`); the assertion is on the finder/repo logic, not on `git` itself. The kitty client is tested with an injected runner that records the argument list. The launcher runs against a fake instance that keeps a window table; the sidebar backend's state table and the `viewed_at` rule run on that fake plus an injected state-file reader. The sidebar package tests its model with a recording fake backend. `instance.Ensure` runs against a fake that answers `Ping` after a scripted number of polls.

There are no integration tests that drive kitty. The live check is by hand: build `./ks`, run `./ks new -n smoke -d /tmp/smoke`, and inspect `kitty @ --to unix:$HOME/.config/ks/kitty.sock ls`.

## Build

```bash
make build      # ./ks
make install    # ~/code/bin/ks (macOS: also ad-hoc signs)
```

`make build` passes `-ldflags "-X github.com/mad01/kitty-session/internal/cli.Version=<git-sha>"` so `ks version` prints the short commit. The fallback is `dev`.

## Formatting and linting

```bash
make fmt    # golines -m 100 --base-formatter=gofumpt -w .
make lint   # golangci-lint run ./...
```

No custom linters or build tags are in use.
