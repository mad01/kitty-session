# Hooks and state detection

`ks` shows a live state badge next to every session: `working`, `idle`, `input`, `done`, `error`, `waiting`, or `stopped`. This doc explains where each value comes from.

The first source is the same for every agent kind: the program's own report through the Program Status Protocol, an Operating System Command (OSC) sequence numbered 7501, when kitty passes it (section 1). Where the rest comes from depends on the session's agent kind (the `agent` field of the record; see [Command reference](commands.md#ks-new)):

- **claude**: the three further sources below, in preference order. This is the bulk of this doc.
- **pi**: the ks pi extension, `pi/ks-agent-state.ts`, which reports pi's lifecycle events to the hidden `ks _pi-hook` command; that writes the same state file and keeps the record's `pi_session_id` and `pi_session_path` current. There is no title glyph and no text classifier for pi, so without a program status record the state file is the only source, whatever its age. See [pi](pi.md).
- **shell**: nothing. A shell session has no state file and shows `idle` while its window is there, `stopped` otherwise. A program in that shell that speaks OSC 7501 reports through section 1 like any agent.

There are four ways `ks` can learn a claude session's state, in preference order:

1. **Program status, OSC 7501** (most accurate; preferred; needs a kitty that passes it)
2. **Claude Code hooks** (most accurate otherwise; always needed for the record)
3. **Background Haiku agent** (optional fallback)
4. **Terminal text heuristics** (always-on fallback)

The last three write or read through the same interface: a state file at `~/.config/ks/state/<session-name>.json`, the file the pi extension writes too. The first never touches it: kitty holds the records and ks reads them from the `kitty @ ls` snapshot.

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

`internal/state/file.go` has one threshold: `freshness = 10 * time.Second`, behind `IsFresh`. A state file younger than that is **fresh**. A fresh file also outranks a program status record (section 1): the hooks saw an event the record may not reflect yet. Past the threshold the record, when kitty lists one, decides; the rules below apply without one.

- `ks list` trusts a fresh file outright, whatever it says. An older file is ignored and the pane text is classified instead (section 4).
- The sidebar lets a fresh `input` or `working` win over the title glyph. Past 10 s the glyph leads, with one exception: an `input` entry counts whatever its age, until a later hook replaces it, so an unanswered prompt stays visible. Its full order is in [Sidebar guide](tui.md#states).

## 1. Program status, OSC 7501 (preferred)

The [Program Status Protocol](https://www.superlogical.com/rex/docs/build/program-status), by Mitchell Hashimoto, lets a program tell its terminal what it is doing. The program writes `OSC 7501 ; state=working:kind=…:msg=<base64> ST` and the terminal keeps the record per window. Claude Code emits it since 2.1.295 and pi since 1.1.0. kitty handles it in master as of 2026-10-13 ([kovidgoyal/kitty#10638](https://github.com/kovidgoyal/kitty/issues/10638)); release 0.49.2 and older ignore it. In the ks instance kitty lists each window's records under `program_status` in `kitty @ ls`, the snapshot the sidebar polls anyway, so this source costs no extra kitty call and no hook. `ks list` reads the same field.

### The probe

Both programs send `OSC 7501 ; ? ST` at startup and wait about two seconds for the terminal's reply. Without one they treat the protocol as unsupported and stay silent for the whole run. On kitty 0.49.2 or older nothing ever arrives, `kitty @ ls` has no `program_status` key at all, and every row falls back to sections 2 to 4. A program started before the instance ran a kitty with the protocol stays silent until it restarts.

### Record fields

| Field | Values | ks |
|---|---|---|
| `state` | `idle`, `working`, `done`, `blocked`, `error` | The one field ks reads; mapped below |
| `kind`, `msg`, `id`, `progress`, `app`, `title` | Per the spec | Ignored |

A window can hold several records (the spec nests them by `id`). `internal/kitty` keeps the most urgent state as `Window.Status`: `blocked` before `error` before `done` before `working` before `idle`. It is empty for a window without records, and a state ks does not know is skipped.

### State → ks map

| Record `state` | Sidebar and `ks list` | Meaning |
|---|---|---|
| `blocked` | `input` | The program cannot continue until you act: a permission prompt, a question, or an auth step |
| `working` | `working` | |
| `done` | `done` | The turn finished and the window has not been looked at since. On the active tab it reads `idle` |
| `error` | `error` | The program reported a failure and stopped |
| `idle` | `idle` | |

The sidebar and `ks list` share this in `launcher.ReportedState`, applied right after the stopped check for every kind (claude, pi and shell). The order: a fresh state file (younger than the 10 s threshold) hands over to the kind's own rules, because the hooks saw an event the record may not reflect yet. Otherwise the record, when kitty lists one, decides. Otherwise the kind's own rules of [Sidebar guide](tui.md#states) apply. The protocol has no `waiting`; that value only ever comes from the `SessionStart` hook or the classifier.

### Lifetime

kitty drops records on its own; ks never writes or clears one.

- `done` and `error` go on the next focus-in or keypress in the window, not on focus-out. ks reads `done` as `idle` while its tab is active, so a result you looked at without typing shows as `done` again once you switch away, until the next visit. `viewed_at` plays no part.
- `working`, `blocked` and `idle` go at a new shell prompt and on a full terminal reset. The prompt is the OSC 133 mark, so only a window running a shell with integration prints one; a claude or pi window has no prompt and keeps its last record until the program replaces it.
- A window's records vanish with the window. A crashed agent leaves nothing behind, and the row shows `stopped` through the window check anyway.

### What the hooks still do

Program status replaces the state file as the badge's source, not the hooks. `SessionStart` is what gives a record its Claude session id and transcript path for `--resume`, and `SessionEnd` what marks a `/exit` `stopped`; neither has a protocol counterpart. The `waiting 12m` age on an `input` row also comes from the hooks' state file (they write `input` for the same prompt), so without them it reads zero. And on a kitty without the protocol the state file is still where the badge comes from. Keep `ks hooks install` in place.

For pi, the records kitty passes take precedence over the file the ks pi extension writes. The extension is unchanged and remains the source on an older kitty (see [pi](pi.md)).

## 2. Claude Code hooks

Claude Code fires [hook events](https://docs.claude.com/en/docs/claude-code/hooks) at specific points in its lifecycle. `ks hooks install` wires six of them to a hidden `ks _hook` handler, which writes the state file and keeps the session record's lifecycle fields current. The state file is the badge's source wherever kitty passes no program status; the record fields are needed everywhere.

### Install / uninstall

```bash
ks hooks install    # writes matcher groups to ~/.claude/settings.json
ks hooks uninstall  # strips them
```

Both commands are idempotent. Install re-runs remove any stale ks entries (for example, entries pointing at an old binary path) before writing the fresh set. Uninstall removes any matcher whose command matches `ks _hook`, whether written with `~/` or an absolute path.

An install made before an event was added to `ks` lacks that event. The sidebar's `hooks status` menu entry reports it as missing; rerun `ks hooks install` to add it.

### Event → state map

| Event | Matcher | State written | Extra effect |
|---|---|---|---|
| `UserPromptSubmit` | *(empty)* | `working` | Fires when you send a message, before any tool call, so a stale `input` clears on your next message even when you dismissed the prompt |
| `PreToolUse` | `.*` | `working` | |
| `Stop` | *(empty)* | `idle` | |
| `Notification` | `permission_prompt\|elicitation_dialog` | `input` | — |
| `SessionStart` | *(empty)* | `waiting` | Stores the payload's `session_id` and `transcript_path` on the record as `claude_session_id` / `claude_transcript_path` and sets `status` to `active` |
| `SessionEnd` | `prompt_input_exit\|logout` | *(none)* | Sets `status` to `stopped` and removes the state file. The handler checks the reason again, so a `clear`, `resume` or `other` that slips through is still ignored |

`Notification` is registered for `permission_prompt` and `elicitation_dialog` only. Claude Code also sends `idle_prompt` after about a minute at an empty prompt. Mapping it to `input` would turn every idle session into a false "waiting on you" and bury the real ones. `done` and `idle` already cover that case. Should another type ever belong here, it is one more alternative in the `Notification` matcher in `internal/hooks/hooks.go` and a case in `stateForEvent` in `internal/cli/hook.go`.

Every launch path (`ks new`, `ks open`, `ks tmp`, the sidebar) exports three variables into both of the session's windows: `KS_SESSION_NAME=<name>`, `KS_SESSION_ID=<id>` and `KS_EXE=<path of the ks binary>` (the pi extension runs `$KS_EXE _pi-hook`, so it reaches the build that made the session). The hook finds the record by `KS_SESSION_ID` first (the `id` field, stable across renames) and falls back to `KS_SESSION_NAME` for records written before ids existed. The state file is keyed by the record's *current* name, so a rename made in the sidebar does not strand later hook writes. If `KS_SESSION_NAME` is unset, the hook exits silently. It is safe to keep installed even in terminals that aren't `ks` sessions.

Two more guards keep the record honest:

- **Nested claudes are ignored.** A `claude` started from inside a session (say `claude -p ...` from the Bash tool) inherits `KS_SESSION_NAME`. The hook therefore checks the process tree: the Claude that fired it must be a direct child of the `kitty` process, which is how `kitty @ launch` starts it. Anything else exits 0 without writing. The lookup lives in `internal/procinfo` and only exists on macOS; elsewhere the hook trusts the environment as before.
- **Subagents write state, not the record.** Events carrying an `agent_id` come from an in-process subagent. Their `PreToolUse`/`Stop` still update the state file, but `SessionStart`/`SessionEnd` from a subagent leave `claude_session_id` and `status` alone.

### Session ID and status

Four fields on the session record (`~/.config/ks/sessions/<name>.json`) outlive kitty restarts:

- `id`: random, assigned by `ks new`/`ks tmp` (or on the first reopen of an older record) and never changed. It is what `KS_SESSION_ID` carries.
- `claude_session_id`: the `session_id` from the most recent `SessionStart` payload. Every source (`startup`, `resume`, `clear`, `compact`, `fork`) updates it, so after a `/clear` the record points at the new conversation.
- `claude_transcript_path`: the `transcript_path` from the same payload. `ks open` on a session whose claude window is gone runs `claude --resume <id>` only while that file still exists. Records without the field fall back to `~/.claude/projects/<encoded dir>/<id>.jsonl`. Claude Code purges transcripts after its cleanup period, and `--resume` then fails with "No conversation found", so a missing file means `claude --continue` instead.
- `status`: `active` or `stopped`. `SessionEnd` with reason `prompt_input_exit` (the user typed `/exit`) or `logout` writes `stopped`. Reason `other` is what Claude reports when kitty closes the window, and `clear`/`resume` are restarts, so those leave the record `active`. `ks close --keep` and the sidebar's close action also write `stopped` before closing the tab. Every launch path saves the record as `active` *before* the first kitty call, so `SessionStart` always finds it. Afterwards it writes only the kitty IDs back, so the hook's update is not clobbered. Records written by older `ks` versions have no `status` field and read as `active`.

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
    "UserPromptSubmit": [ /* ... */ ],
    "Stop":             [ /* ... */ ],
    "Notification":     [ /* matcher "permission_prompt|elicitation_dialog" */ ],
    "SessionStart":     [ /* ... */ ],
    "SessionEnd":       [ /* matcher "prompt_input_exit|logout" */ ]
  }
}
```

Existing non-ks hooks in the file are preserved.

## 3. Background Haiku agent (optional fallback)

Launched by `ks sidebar --agent`, or by the home sidebar when `ks --agent` starts the instance. The agent is a long-running `claude` invocation with a hardened system prompt and a tightly restricted `--allowedTools` list. It loops every five seconds:

1. List session files in `~/.config/ks/sessions/`.
2. For each one with a live kitty window ID, run `kitty @ get-text --match=id:<id>`.
3. Classify the terminal text into `working` / `idle` / `input` / `waiting`.
4. Write the result to `~/.config/ks/state/<name>.json`.

The agent uses the `haiku` model alias. Its allowed tools are just `Bash(kitty @ --to <socket> get-text *)`, `Bash(ls ...)`, `Bash(sleep *)`, plus read/write access to the session and state directories. The system prompt itself forbids launchd/cron/plist/scripts. When that sidebar exits, `ks` kills the agent's process group so nothing lingers.

Use the agent if you don't want to install the Claude Code hooks, or as a belt-and-braces setup alongside hooks.

## 4. Title glyph and terminal text (always-on)

The sidebar reads the claude window's title from the `kitty @ ls` snapshot it already takes. Claude Code prefixes that title with a glyph: a spinner (`◐◓◑◒`, `✢✶✻✽`, or a braille pattern) while it works and `✳` at the prompt. `internal/claude.ParseTitle` turns that into `working` or `idle`; the rest of the title is the row's second line. How the sidebar combines it with the state file is in [Sidebar guide](tui.md#states).

`ks list` instead reads the Claude pane via `kitty @ get-text` and runs a classifier (`internal/claude.DetectState`). It looks at the last 50 non-empty lines bottom-up:

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

The recommended setup is hooks alone. The agent is there for when you can't or don't want to modify `~/.claude/settings.json`. On a kitty that passes program status, neither decides the badge for a claude or pi that speaks the protocol. The hooks still carry the session id and the stopped status, so they stay recommended.

## Inspecting state

```bash
ls ~/.config/ks/state/
cat ~/.config/ks/state/<name>.json
kitty @ --to unix:$HOME/.config/ks/kitty.sock ls   # program_status under each window
```

`ks list` prints the current state alongside the name, the agent kind and the directory. Both go through `launcher.ReportedState`, so with a record and no fresh state file they agree. Without one the sidebar's dot is computed separately (it adds `done`, reads the title glyph instead of the pane text, and keeps `input` until a later hook replaces it), so the two can differ. For a pi session both then read the state file alone; for a shell both say `idle`.

## Cleaning state

- `ks close <name>` (with or without `--keep`) and the sidebar's close and delete actions all go through one helper, `launcher.Close`. It removes the state file, closes the session's live tabs, and then either marks the record `stopped` or moves it to the trash.
- The `SessionEnd` hook removes the state file when it marks a record `stopped`.
- Stale state for sessions that no longer exist is harmless. `ks list` and the sidebar only look up state for sessions that are present in the session store. A record whose `status` is `stopped` shows `stopped` without consulting kitty or the state file.
