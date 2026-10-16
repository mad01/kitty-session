# Troubleshooting

Things that go wrong and how to fix them.

## `cannot start ks instance` or `did not answer ... within 10s`

Symptom: `ks`, `ks new` or `ks open` fails before any tab appears.

`ks` starts its own kitty with `kitty --detach --listen-on unix:<socket> ...` and waits up to ten seconds for the socket to answer. Check, in order:

- `kitty` is on `PATH` in the shell you run `ks` from: `kitty --version`.
- The socket directory exists and is writable: the default is `~/.config/ks/kitty.sock`; see `kitty_socket` in [Configuration](configuration.md).
- Nothing else owns that path. `ks` removes a stale socket file before starting, but a foreign process listening there keeps answering `ls` and `ks` will happily talk to it.
- Your `~/.config/kitty/kitty.conf` loads. The instance reads it; a syntax error there shows up in kitty's own output, not in `ks`. Try `kitty --detach -o allow_remote_control=yes --listen-on unix:/tmp/probe.sock -- sleep 60` and `kitty @ --to unix:/tmp/probe.sock ls` by hand.

## `ks instance not running`

Printed by `ks list` (after every session, all shown as `stopped`) and by `ks quit`. Nothing is wrong: the instance is down. `ks`, `ks new`, `ks open` and `ks tmp` start it; `ks list`, `ks close`, `ks rename` and `ks quit` never do.

## `claude: not found`

Symptom: a session tab appears, the claude window flashes and closes, the sidebar shows the session as `stopped`.

Fix: install [Claude Code](https://docs.claude.com/en/docs/claude-code/overview) and make sure `claude` is on the `PATH` of the shell you run `ks` from. Every launch path forwards that `PATH` into the session's windows, so a `claude` under `~/.local/bin` works even though `kitty @ launch` runs with the instance's environment. The `--agent` flag prints `warning: agent failed to start: claude not found in PATH` for the same reason.

## Claude exits right after `ks` or `ks open`

Symptom: the tab closes again within seconds, or comes back with the sidebar only; the row is gone, or `ks list` says `stopped`.

Bare `ks` reports this as `ks: <name> exited right after launch`. A reopen starts `claude --resume <id>` when the record has a Claude session id whose transcript file still exists, `claude --continue` when the directory has any transcript, and a bare `claude` otherwise. A missing conversation is not the cause. Look at what Claude printed before the window closed. The sidebar closes a tab whose claude is gone within a moment of a `/exit`, within a few seconds of a crash, and after 30 seconds when it never saw the window. Until then `ks open <name>` puts a fresh claude beside it, with the message visible for a moment. A close that lands after that relaunch takes the new claude with it, so wait for the tab to go before reopening. The record stays `active` when claude died within ten seconds of showing up, as it does here. A claude that ran longer is moved to the trash (see [When the agent exits](tui.md#when-the-agent-exits)), and the sidebar's `u` restores it. Usual causes are a `claude` that is not on the `PATH` of the shell you ran `ks` from, or a transcript Claude Code purged between the `ls` check and the start.

Older `ks` builds passed the environment of a Claude Code session straight into the instance. That turned every claude in it into a child session with transcript saving off, and the footer said so. Current builds scrub those variables; if you still see that footer, `ks quit` and start the instance with the new binary.

## Session shows `stopped` but its tab is still open

The sidebar and `ks list` only count a claude window as the session's when the window carries the kitty user variable `KS_SESSION_ID` matching the record's `id` and its id matches `kitty_window_id`. A tab you created by hand in the instance, or a window from an older `ks` version, does not qualify.

Fix: close the orphaned tab by hand, then `ks open <name>`.

## No sessions after a reboot

Session files are never deleted by `ks` on shutdown; they persist across reboots and across `ks quit`. Run `ks` to bring every active one back. If the sidebar shows nothing:

- Verify the files exist: `ls ~/.config/ks/sessions/`.
- Check the trash: `ls ~/.config/ks/sessions/trash/`. Restore from the sidebar with `u`.

## `no config found (checked ~/.config/ks/config.yaml)`

Cause: `ks repo` and the sidebar's repo picker require a config file with at least one `dirs` entry. Every other command works without one.

Fix: create the file. Minimal example:

```yaml
dirs:
  - ~/code
```

See [Configuration](configuration.md).

## `no git repositories found`

`ks repo` exits with this error when the walker finishes scanning `dirs` and finds nothing. Usually one of:

- None of the configured `dirs` exist on disk.
- They exist but contain no `.git` directories.
- Every repo lives in a directory whose name starts with `.` (those are skipped).

Check with `ls <configured-dir>` and confirm you expected repos there.

## No `program_status` on kitty 0.49.2 or older

Claude Code (2.1.295 and later) and pi (1.1.0 and later) report their state to the terminal through the Program Status Protocol (Operating System Command 7501). They do so only after kitty answers their startup probe, and releases up to 0.49.2 do not. The sign: `kitty @ --to unix:$HOME/.config/ks/kitty.sock ls` has no `program_status` key under any window (verified on 0.48.2). A kitty that supports the protocol lists one for every window, an empty list for a program that stays silent. `error` never appears in the sidebar either. Nothing is broken: the badge comes from the hooks, the title glyph and the pane text as before. For the protocol, run a kitty built from master after 2026-10-13 ([kovidgoyal/kitty#10638](https://github.com/kovidgoyal/kitty/issues/10638)) or the next release, then `ks quit` and start again. A claude started under the old kitty stays silent until it restarts. See [Hooks and state detection](hooks-and-state.md#1-program-status-osc-7501-preferred).

## State badge is always `waiting`

Your kitty passes no program status (above), the hooks are not firing in that claude, and you aren't running the `--agent` monitor. The fallback terminal classifier is conservative. It only returns `working` when it sees one of the specific signal words or a spinner character, and only `idle` when the last line is exactly `>`. Anything else becomes `waiting`.

Check `ks hooks status`, then close the tab (`c` in the sidebar) and reopen the session so a fresh claude starts with the injected settings. The checks under [Hooks not firing](#hooks-not-firing) cover the rest. See [Hooks and state detection](hooks-and-state.md).

## State badge stuck on `working`

On a kitty that passes program status the record the agent last wrote decides once the state file is older than 10 seconds; look at `program_status` in the `ls` output first. A claude or pi window has no shell prompt, so kitty keeps its `working` record until the program replaces it. Without a record the file rules apply.

A `working` state file is trusted for 10 seconds. After that `ks list` classifies the pane text and the sidebar follows the title glyph. A badge stuck on `working` past that means the pane still reads as busy, or a spinner glyph is still in the title.

If Claude actually finished, the `Stop` hook should have overwritten the state file with `idle`. Check:

```bash
cat ~/.config/ks/state/<name>.json
```

If the `state` field isn't `idle` and `updated_at` is older than the last Claude Code finish, the hooks aren't firing. Work through the next section.

## Hooks not firing

The hooks reach claude through one `--settings <inline JSON>` argument on its command line, not through `~/.claude/settings.json`. Things to check:

- This version of `ks` started the claude. A claude started before `make install` keeps the settings it started with; close the tab (`c` in the sidebar) and reopen the session (`ks open <name>`, or `enter` on its row).
- The argument is there. `kitty @ --to unix:$HOME/.config/ks/kitty.sock ls` shows the claude window's `cmdline`; it should contain `--settings` followed by JSON with a `hooks` key, and the command inside names the ks binary that made the session. `ps -o args= -p <pid>` shows the same.
- The installed claude accepts inline JSON for `--settings`. Claude Code 2.1.x does; `claude --help` lists `--settings <file-or-json>`.
- `KS_SESSION_NAME` must be set in the env. If you launched a claude some other way (not via `ks`), the hook exits silently.
- Your own hooks in `~/.claude/settings.json` keep running beside the injected ones. `ks hooks status` reports leftover `ks _hook` entries an older version wrote there; `ks hooks uninstall` removes them. They cost a duplicate run, nothing more.

## Sidebar is too narrow or wraps

The sidebar is `sidebar_width` cells wide (default 36, minimum 20) and draws its frame at that width, narrower only when the window is. Raise `sidebar_width` in [Configuration](configuration.md); it applies when a tab is created or claude is relaunched into it, and the sidebar re-pins itself after a window resize.

## Scratch session not being cleaned up

`ks` doesn't clean scratch directories. They live under the `tmpdir` you configured (or the OS tmp directory). With the default `tmpdir`, macOS and Linux will clean `/tmp` eventually; with a custom `tmpdir` you're responsible for clearing it.

Recipe (adapt as needed):

```bash
# Remove scratch workspaces older than 14 days
find ~/.config/ks/claude-session-workspaces -maxdepth 1 -type d -mtime +14 -name 'ks-*' -exec rm -rf {} +
```
