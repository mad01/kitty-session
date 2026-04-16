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

Claude Code fires [hook events](https://docs.claude.com/en/docs/claude-code/hooks) at specific points in its lifecycle. `ks hooks install` wires four of them to a hidden `ks _hook` handler, which writes the state file.

### Install / uninstall

```bash
ks hooks install    # writes matcher groups to ~/.claude/settings.json
ks hooks uninstall  # strips them
```

Both commands are idempotent. Install re-runs remove any stale ks entries (for example, entries pointing at an old binary path) before writing the fresh set. Uninstall removes any matcher whose command matches `ks _hook`, whether written with `~/` or an absolute path.

### Event → state map

| Event | Matcher | State written | Extra effect |
|---|---|---|---|
| `PreToolUse` | `.*` | `working` | On `EnterPlanMode` or `ExitPlanMode`, sends `refresh\n` to the session's summary tab (if any) |
| `Stop` | *(empty)* | `idle` | Sends `refresh\n` to the summary tab |
| `Notification` | `permission_prompt\|elicitation_dialog` | `input` | — |
| `SessionStart` | *(empty)* | `waiting` | — |

`KS_SESSION_NAME` is exported by `ks new` and `ks open` when they launch the kitty tab (`--env KS_SESSION_NAME=<name>`). The hook uses that env var to know which state file to write. If `KS_SESSION_NAME` is unset, the hook exits silently — it's safe to keep installed even in terminals that aren't `ks` sessions.

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
    "Notification": [ /* ... */ ],
    "SessionStart": [ /* ... */ ]
  }
}
```

Existing non-ks hooks in the file are preserved.

## 2. Background Haiku agent (optional fallback)

Launched by `ks --agent` from the TUI root command. The agent is a long-running `claude` invocation with a hardened system prompt and a tightly restricted `--allowedTools` list. It loops every five seconds:

1. List session files in `~/.config/ks/sessions/`.
2. For each one with a live kitty window ID, run `kitty @ get-text --match=id:<id>`.
3. Classify the terminal text into `working` / `idle` / `input` / `waiting`.
4. Write the result to `~/.config/ks/state/<name>.json`.

The agent uses the `haiku` model alias. Its allowed tools are just `Bash(kitty @ get-text *)`, `Bash(ls ...)`, `Bash(sleep *)`, plus read/write access to the session and state directories. The system prompt itself forbids launchd/cron/plist/scripts. When the TUI exits, `ks` kills the agent's process group so nothing lingers.

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
| Lifespan | Permanent until uninstalled | Only while `ks` (TUI with `--agent`) is running |
| Per-session opt-out | Via `KS_SESSION_NAME` being unset | Per-session; skips windows without a kitty ID |

The recommended setup is hooks alone. The agent is there for when you can't or don't want to modify `~/.claude/settings.json`.

## Inspecting state

```bash
ls ~/.config/ks/state/
cat ~/.config/ks/state/<name>.json
```

`ks list` prints the current state alongside the name and directory. The TUI badge is the same value — computed via the same function.

## Cleaning state

- `ks close <name>` removes the session's state file.
- `ks delete` (from the TUI) does the same before trashing the session record.
- Stale state for sessions that no longer exist is harmless — `ks list` and the TUI only look up state for sessions that are present in the session store.
