# Sessions running pi

`ks` can host the [pi coding agent](https://github.com/badlogic/pi-mono) in a session instead of Claude Code: the sidebar on the left, `pi` on the right. The record carries `"agent": "pi"`, which the launcher reads to build the command for the right-hand window; a record without the field is a claude session. This page covers what differs for pi: state, resume, install, and what pi can't do.

## State

pi has no hooks and sets no title glyph, so none of the three sources in [Hooks and state detection](hooks-and-state.md) see it. Its state comes from a pi extension instead, `pi/ks-agent-state.ts` in this repo. The extension listens to pi's lifecycle events and runs a hidden `ks _pi-hook` for each one, with a JSON payload on stdin:

```json
{"event": "session_start", "reason": "startup", "session_id": "...", "session_file": "/Users/you/.pi/agent/sessions/--repo--/2026-10-08T10-00-00_....jsonl"}
```

`ks _pi-hook` writes the same state file the claude hook writes, `~/.config/ks/state/<name>.json`, and keeps the record's pi fields current. The sidebar then reads it as it does for claude, minus the glyph. With no title to lean on, the state file is the row's only signal. A `done` dot appears when the file's `idle` is newer than the record's `viewed_at`.

### Event table

| pi event | Sent when | State written | Extra effect |
|---|---|---|---|
| `session_start` | pi's TUI starts, resumes, forks or reloads | `waiting` | Stores `session_id` and `session_file` on the record as `pi_session_id` / `pi_session_path` and sets `status` to `active` |
| `agent_start` | A run begins | `working` | |
| `tool_call` | Each tool call | `working` | The keepalive: it keeps the state file inside the 10 s freshness window while a long run makes no other event, like claude's `PreToolUse`. Not sent while a prompt waits |
| `agent_settled` | A run has fully ended and pi is idle | `idle` | Sent only when `ctx.isIdle()` agrees |
| `blocked` | The permission-gate extension opens a prompt (`herdr:blocked` with `active: true`) | `input` | Sticky in the sidebar until a later write |
| `unblocked` | The last open prompt is answered | `working` | A denied call still ends the run through `agent_settled`, so this never has to guess `idle` |
| `session_shutdown` | pi tears its runtime down: `quit`, `reload`, `new`, `resume`, `fork` | *(none)* | Removes the state file. The record stays `active` |

Two gates sit in front of all of it. The extension does nothing unless `KS_SESSION_ID` is in its environment, which only the launcher sets, and it reports only from pi's TUI (`ctx.mode === "tui"`). A nested `pi -p` started from inside the session inherits the variable but runs headless, so it never writes. That is why `ks _pi-hook` has no process-tree guard where `ks _hook` has one.

The handler keys the state file by the record's current name, found through `KS_SESSION_ID` first and `KS_SESSION_NAME` second, exactly like `ks _hook`. Record problems go to stderr as `ks _pi-hook: <error>` and the command still exits 0.

### Why `session_shutdown` never marks a record stopped

The claude hook marks a record `stopped` on a `SessionEnd` whose reason is `prompt_input_exit` or `logout`, so that a session you left on purpose stays out of the next attach. pi offers no such distinction. Its `session_shutdown` carries `reason: "quit"` for every teardown that ends the process, and the interactive mode runs the same teardown from its `SIGTERM`/`SIGHUP` handler as from `/quit` and `ctrl+d`. `ks quit` ends every session with a `SIGHUP`, so a `quit` reason would mark every pi session stopped and none would come back. So `ks _pi-hook` only removes the state file, for every reason. A pi session you ended with `/quit` stays `active`: its sidebar closes the tab once pi is gone, and the next `ks` resumes it. Close it with `ks close <name>` or the sidebar's `c` when you mean it to stay down.

## Resume

A reopen (bare `ks`, `ks open`, the sidebar's `enter` on a stopped row) runs `pi --session <file>` when the record's `pi_session_path` still exists, and a bare `pi` otherwise. pi keeps its sessions under `~/.pi/agent/sessions/`, one directory per working directory, and the extension records the file on every `session_start`, so a `/new`, `/resume` or `/fork` inside pi moves the record along with it. A session that never got a message comes back as a fresh conversation, as with claude.

## Install

Three things have to be in place:

1. `pi` on the PATH of whoever runs `ks`. Every launch forwards `PATH` into the session's windows, so a `pi` under `~/.local/bin` or a mise shim works too.
2. The extension in `~/.pi/agent/extensions/`. The dotfiles recipe symlinks `pi/ks-agent-state.ts` from the ks checkout into that directory; pi loads every file there at startup. Without it nothing writes the state file, so a pi row stays `idle` whatever pi is doing.
3. `KS_EXE` in the session's environment, set by the launcher to the `ks` binary that opened the session. The extension runs that binary, so a branch build of `ks` reports to itself rather than to whatever `ks` is first on PATH. Unset, it falls back to `ks`.

Check an open session with:

```bash
cat ~/.config/ks/state/<name>.json
```

A pi session that never gets past `waiting` after you send a message has no working extension. Look for it with `ls -l ~/.pi/agent/extensions/`, then run `pi` by hand in the tab's directory and watch for an extension error on startup.

## Limitations

- **No title glyph.** The sidebar's second line shows the session directory, never a task title, and `ks list` has nothing to classify: it prints whatever the state file says, at any age, and `idle` without one.
- **No `--agent` monitor.** The background Haiku classifier knows Claude Code's screen, not pi's. Install the extension instead.
- **`ks import` stays claude-only.** herdr records for pi panes are skipped.
- **Unverified on `SIGHUP`.** pi's source says its signal handler runs the shutdown handlers before exiting, which is what the state-file removal relies on; the extension hasn't been exercised against a real `ks quit` yet. Should the state file survive one, nothing breaks: a record whose window is gone shows `stopped` whatever the file says.
