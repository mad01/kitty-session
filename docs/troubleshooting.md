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

Symptom: the tab comes back with the sidebar only; `ks list` says `stopped`.

Bare `ks` reports this as `ks: <name> exited right after launch`. A reopen starts `claude --resume <id>` when the record has a Claude session id whose transcript file still exists, `claude --continue` when the directory has any transcript, and a bare `claude` otherwise. A missing conversation is not the cause. Look at what Claude printed before the window closed: `ks open <name>` again puts a fresh claude beside the surviving sidebar, and the message is visible for a moment. Usual causes are a `claude` that is not on the `PATH` of the shell you ran `ks` from, or a transcript Claude Code purged between the `ls` check and the start.

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

## State badge is always `waiting`

You haven't installed the Claude Code hooks and you aren't running the `--agent` monitor. The fallback terminal classifier is conservative. It only returns `working` when it sees one of the specific signal words or a spinner character, and only `idle` when the last line is exactly `>`. Anything else becomes `waiting`.

Fix: `ks hooks install`. See [Hooks and state detection](hooks-and-state.md).

## State badge stuck on `working`

The state file says `working` and is less than five minutes old. `ks` trusts that until the terminal clearly shows `idle` or `input`.

If Claude actually finished and the hooks were installed, the `Stop` hook should have overwritten the state file with `idle`. Check:

```bash
cat ~/.config/ks/state/<name>.json
```

If the `state` field isn't `idle` and `updated_at` is older than the last Claude Code finish, the hooks probably aren't firing. Re-run `ks hooks install` and inspect `~/.claude/settings.json`.

## `ks hooks install` then still nothing changes

`ks hooks install` edits `~/.claude/settings.json`. Things to check:

- Claude Code reads this file at start. Running instances won't pick up new hooks; stop and restart `claude` (`ks open` after a `c` in the sidebar does that).
- The installed command looks like `~/code/bin/ks _hook`. If your `ks` binary lives elsewhere, re-run install after moving it so the path is correct.
- `KS_SESSION_NAME` must be set in the env. If you launched a claude some other way (not via `ks`), the hook exits silently.

## Sidebar is too narrow or wraps

The sidebar is `sidebar_width` cells wide (default 36, minimum 20) and the TUI clamps its inner size to 150×50. Raise `sidebar_width` in [Configuration](configuration.md); it applies when a tab is created or claude is relaunched into it.

## Scratch session not being cleaned up

`ks` doesn't clean scratch directories. They live under the `tmpdir` you configured (or the OS tmp directory). With the default `tmpdir`, macOS and Linux will clean `/tmp` eventually; with a custom `tmpdir` you're responsible for clearing it.

Recipe (adapt as needed):

```bash
# Remove scratch workspaces older than 14 days
find ~/.config/ks/claude-session-workspaces -maxdepth 1 -type d -mtime +14 -name 'ks-*' -exec rm -rf {} +
```
