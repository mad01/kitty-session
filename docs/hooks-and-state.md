# Hooks and state detection

`ks` shows a live state badge next to every session: `working`, `idle`, `input`, `waiting`, or `stopped`. This doc explains where each value comes from.

There are three ways `ks` can learn a session's state, in preference order:

1. **Claude Code hooks** (most accurate; preferred)
2. **Background Haiku agent** (optional fallback)
3. **Terminal text heuristics** (always-on fallback)

All three write or read through the same interface: a state file at `~/.config/ks/state/<session-name>.json`.

## State file

```json
{
  "state": "working",
  "updated_at": "2026-04-16T19:55:01.234Z"
}
```

- `state` — one of `working`, `idle`, `input`, `waiting`.
- `updated_at` — RFC 3339 UTC timestamp.

### Freshness

- Anything within 10 seconds is **fresh** and trusted outright.
- A fresh `working` entry is trusted directly.
- A stale `working` entry less than 5 minutes old is still honored *unless* terminal text clearly says otherwise (idle prompt or visible permission prompt).
- Any state older than 10 seconds that isn't `working` falls through to terminal detection.

The two thresholds live in `internal/state/file.go` (`freshness = 10 * time.Second`, `IsRecentlyWorking` = 5 minutes).

## 1. Claude Code hooks (preferred)

Claude Code fires [hook events](https://docs.claude.com/en/docs/claude-code/hooks) at specific points in its lifecycle. `ks hooks install` wires five of them to a hidden `ks _hook` handler, which writes the state file and keeps the session record's lifecycle fields current.

### Install / uninstall

```bash
ks hooks install    # writes matcher groups to ~/.claude/settings.json
ks hooks uninstall  # strips them
```

Both commands are idempotent. Install re-runs remove any stale ks entries (for example, entries pointing at an old binary path) before writing the fresh set. Uninstall removes any matcher whose command matches `ks _hook`, whether written with `~/` or an absolute path.

### Event → state map

| Event | Matcher | State written | Extra effect |
|---|---|---|---|
| `PreToolUse` | `.*` | `working` | |
| `Stop` | *(empty)* | `idle` | |
| `Notification` | `permission_prompt\|elicitation_dialog` | `input` | — |
| `SessionStart` | *(empty)* | `waiting` | Stores the payload's `session_id` and `transcript_path` on the record as `claude_session_id` / `claude_transcript_path` and sets `status` to `active` |
| `SessionEnd` | `prompt_input_exit\|logout` | *(none)* | Sets `status` to `stopped` and removes the state file. The handler checks the reason again, so a `clear`, `resume` or `other` that slips through is still ignored |

Every launch path (`ks new`, `ks open`, `ks tmp`, the TUI) exports two variables into both of the session's windows: `KS_SESSION_NAME=<name>` and `KS_SESSION_ID=<id>`. The hook finds the record by `KS_SESSION_ID` first (the `id` field, stable across renames) and falls back to `KS_SESSION_NAME` for records written before ids existed. The state file is keyed by the record's *current* name, so a rename made in the TUI does not strand later hook writes. If `KS_SESSION_NAME` is unset, the hook exits silently. It is safe to keep installed even in terminals that aren't `ks` sessions.

Two more guards keep the record honest:

- **Nested claudes are ignored.** A `claude` started from inside a session (say `claude -p ...` from the Bash tool) inherits `KS_SESSION_NAME`. The hook therefore checks the process tree: the Claude that fired it must be a direct child of the `kitty` process, which is how `kitty @ launch` starts it. Anything else exits 0 without writing. The lookup lives in `internal/procinfo` and only exists on macOS; elsewhere the hook trusts the environment as before.
- **Subagents write state, not the record.** Events carrying an `agent_id` come from an in-process subagent. Their `PreToolUse`/`Stop` still update the state file, but `SessionStart`/`SessionEnd` from a subagent leave `claude_session_id` and `status` alone.

### Session ID and status

Four fields on the session record (`~/.config/ks/sessions/<name>.json`) outlive kitty restarts:

- `id`: random, assigned by `ks new`/`ks tmp` (or on the first reopen of an older record) and never changed. It is what `KS_SESSION_ID` carries.
- `claude_session_id`: the `session_id` from the most recent `SessionStart` payload. Every source (`startup`, `resume`, `clear`, `compact`, `fork`) updates it, so after a `/clear` the record points at the new conversation.
- `claude_transcript_path`: the `transcript_path` from the same payload. `ks open` on a session whose claude window is gone runs `claude --resume <id>` only while that file still exists. Records without the field fall back to `~/.claude/projects/<encoded dir>/<id>.jsonl`. Claude Code purges transcripts after its cleanup period, and `--resume` then fails with "No conversation found", so a missing file means `claude --continue` instead.
- `status`: `active` or `stopped`. `SessionEnd` with reason `prompt_input_exit` (the user typed `/exit`) or `logout` writes `stopped`. Reason `other` is what Claude reports when kitty closes the window, and `clear`/`resume` are restarts, so those leave the record `active`. `ks close --keep` and the TUI close action also write `stopped` before closing the tab. Every launch path saves the record as `active` *before* the first kitty call, so `SessionStart` always finds it. Afterwards it writes only the kitty IDs back, so the hook's update is not clobbered. Records written by older `ks` versions have no `status` field and read as `active`.

The state file and the record are updated independently: a failure in one does not skip the other. Record problems (missing, unreadable, unsaveable) are printed to stderr as `ks _hook: <error>` and the hook still exits 0, so Claude's hook run never fails because of `ks`.

### What gets written to `settings.json`

Simplified example after `ks hooks install`:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": ".*",
        "hooks": [
          {"type": "command", "command": "~/code/bin/ks _hook"}
        ]
      }
    ],
    "Stop":         [ /* ... */ ],
    "Notification": [ /* matcher "permission_prompt|elicitation_dialog" */ ],
    "SessionStart": [ /* ... */ ],
    "SessionEnd":   [ /* matcher "prompt_input_exit|logout" */ ]
  }
}
```

Existing non-ks hooks in the file are preserved.

## 2. Background Haiku agent (optional fallback)

Launched by `ks sidebar --agent`, or by the home sidebar when `ks --agent` starts the instance. The agent is a long-running `claude` invocation with a hardened system prompt and a tightly restricted `--allowedTools` list. It loops every five seconds:

1. List session files in `~/.config/ks/sessions/`.
2. For each one with a live kitty window ID, run `kitty @ get-text --match=id:<id>`.
3. Classify the terminal text into `working` / `idle` / `input` / `waiting`.
4. Write the result to `~/.config/ks/state/<name>.json`.

The agent uses the `haiku` model alias. Its allowed tools are just `Bash(kitty @ --to <socket> get-text *)`, `Bash(ls ...)`, `Bash(sleep *)`, plus read/write access to the session and state directories. The system prompt itself forbids launchd/cron/plist/scripts. When the TUI exits, `ks` kills the agent's process group so nothing lingers.

Use the agent if you don't want to install the Claude Code hooks, or as a belt-and-braces setup alongside hooks.

## 3. Terminal text heuristics (always-on)

Both `ks list` and the TUI fall back to reading the Claude pane via `kitty @ get-text` and running a classifier (`internal/claude.DetectState`). It looks at the last 50 non-empty lines bottom-up:

- If the very last line is `>`, the state is `idle`.
- If any line contains `(y/n)`, `do you want to`, or `allow`/`approve` plus `yes`/`no`, the state is `input`.
- If any line mentions the Claude Code welcome screen, the state is `waiting`.
- If any line contains a "working" signal (verbs like `reading`, `editing`, `running`, `compiling`, or a spinner character from `⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏`), the state is `working`.
- Otherwise the state is `waiting`.

This is necessarily fuzzy. Claude's UI changes and a substring match can get tricked. If you care about accuracy, install the hooks.

## Choosing between hooks and the agent

|  | Hooks (`ks hooks install`) | Agent (`ks --agent`) |
|---|---|---|
| Accuracy | High — driven by Claude Code internals | Medium — driven by terminal classification |
| Latency | Immediate (hook fires synchronously) | Up to ~5 seconds |
| Cost | Zero extra model calls | Ongoing Haiku usage while `ks` runs |
| Lifespan | Permanent until uninstalled | Only while the sidebar that started it runs |
| Per-session opt-out | Via `KS_SESSION_NAME` being unset | Per-session; skips windows without a kitty ID |

The recommended setup is hooks alone. The agent is there for when you can't or don't want to modify `~/.claude/settings.json`.

## Inspecting state

```bash
ls ~/.config/ks/state/
cat ~/.config/ks/state/<name>.json
```

`ks list` prints the current state alongside the name and directory. The TUI badge is the same value — computed via the same function.

## Cleaning state

- `ks close <name>` (with or without `--keep`) and the TUI close and delete actions all go through one helper, `launcher.Close`. It removes the state file, closes the session's live tabs, and then either marks the record `stopped` or moves it to the trash.
- The `SessionEnd` hook removes the state file when it marks a record `stopped`.
- Stale state for sessions that no longer exist is harmless. `ks list` and the TUI only look up state for sessions that are present in the session store. A record whose `status` is `stopped` shows `stopped` without consulting kitty or the state file.
