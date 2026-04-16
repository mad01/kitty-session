# Troubleshooting

Things that go wrong and how to fix them.

## `kitty @` commands fail

Symptom: `ks new` or `ks open` prints `kitty @ launch tab: ...` errors; the TUI shows errors in the status bar when you press `o`.

Cause: kitty doesn't allow remote control by default.

Fix: edit `~/.config/kitty/kitty.conf` and add:

```
allow_remote_control yes
listen_on unix:/tmp/mykitty
```

Restart kitty. Confirm with:

```bash
kitty @ ls
```

It should print JSON. If it prints an auth error, check kitty's [remote control docs](https://sw.kovidgoyal.net/kitty/remote-control/) for token-based setups.

## `claude: not found`

Symptom: `ks new` creates a tab but Claude never starts; the shell pane shows `claude: command not found`. The `--agent` flag prints `warning: agent failed to start: claude not found in PATH`. Summary tab fails to launch.

Fix: install [Claude Code](https://docs.claude.com/en/docs/claude-code/overview) and make sure `claude` is on the `PATH` that kitty itself sees when it launches. `kitty @ launch` uses kitty's environment, not your current shell's, so exporting `PATH` in `~/.zshrc` doesn't automatically apply. The TUI path explicitly forwards your `PATH` via `--env PATH=<current-PATH>`; the `ks new` and `ks open` subcommands do not. If `claude` lives in a shell-only location, create sessions via the TUI or add the directory to kitty's launch environment (`launch_env` in `kitty.conf`).

## Session shows `stopped` but the kitty tab is still open

Cause: the `kitty_tab_id` stored in `~/.config/ks/sessions/<name>.json` no longer matches any live tab. This can happen after a kitty restart — IDs don't survive across `kitty` process boundaries.

Fix: close the orphaned kitty tab manually, then `ks open <name>`. `ks` will see the stored tab ID is stale, launch a new tab via `claude --continue`, and update the session file with the new IDs.

Alternative: open `~/.config/ks/sessions/<name>.json`, read the new tab ID from `kitty @ ls`, and edit it in by hand.

## No sessions after a reboot

Session files are never deleted by `ks` on shutdown — they persist across reboots. If you can't see them:

- Verify the files exist: `ls ~/.config/ks/sessions/`.
- The TUI shows all files in that directory as sessions. If the directory is empty, you had no saved sessions.
- Check the trash: `ls ~/.config/ks/sessions/trash/`. Restore them from the TUI with `u`.

## `no config found (checked ~/.config/ks/config.yaml)`

Cause: `ks repo` and the TUI repo picker require a config file with at least one `dirs` entry. `ks new` and `ks open` will work without one (they fall back to the default `split` layout and no summary tab).

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

You haven't installed the Claude Code hooks and you aren't running `ks --agent`. The fallback terminal classifier is conservative — it only returns `working` when it sees one of the specific signal words or a spinner character, and only returns `idle` when the last line is exactly `>`. Anything else becomes `waiting`.

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

- Claude Code reads this file at start. Running instances won't pick up new hooks — stop and restart `claude`.
- The installed command looks like `~/code/bin/ks _hook`. If your `ks` binary lives elsewhere, re-run install after moving it so the path is correct.
- `KS_SESSION_NAME` must be set in the env. If you launched a session some other way (not via `ks new` or `ks open`), the hook will exit silently.

## TUI looks cramped or wraps

The TUI clamps its inner size to 150×50 and shrinks to a minimum of 40×10. If your terminal is between those, you're in normal territory. If it's smaller, the frame rendering will wrap.

Fix: widen the kitty window, or reduce the font size.

## Summary tab never appears

- Confirm the config actually enables it: `layout: tab` *and* `summary: true`. With `layout: split`, summary is silently disabled.
- Confirm `claude` is on `PATH` — the summary agent is `claude` with a restricted allow-list.
- Check for a stderr warning when creating a session: `warning: could not create summary tab: ...`.

If the tab appears but is empty for more than 10 seconds, the `refresh\n` nudge may have been sent before `claude` finished starting up. Close and recreate the session; the 3-second initial delay usually covers it.

## Scratch session not being cleaned up

`ks` doesn't clean scratch directories. They live under the `tmpdir` you configured (or the OS tmp directory). With the default `tmpdir`, macOS and Linux will clean `/tmp` eventually; with a custom `tmpdir` you're responsible for clearing it.

Recipe (adapt as needed):

```bash
# Remove scratch workspaces older than 14 days
find ~/.config/ks/claude-session-workspaces -maxdepth 1 -type d -mtime +14 -name 'ks-*' -exec rm -rf {} +
```
