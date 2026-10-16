# Hooks and state detection

`ks` shows a live state badge next to every session: `working`, `idle`, `input`, `done`, `error`, `waiting`, or `stopped`. This doc explains where each value comes from.

The desktop notification a session's sidebar posts when its row turns `input` (or `done`, with `notify: [input, done]` in the config) is read off the same resolved state as the badge. Whatever source produced the `input` (a hook, a program status record, the screen check) produces the banner too; see [Sidebar guide](tui.md#notifications).

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

- `state`: one of `working`, `idle`, `input`, `error`, `waiting`.
- `updated_at`: RFC 3339 UTC timestamp.
- `tool`: optional, present only on an `input` entry: the tool whose prompt is waiting (`Bash`, `AskUserQuestion`, and so on). `PostToolUse` and `PostToolUseFailure` clear the entry only when their tool matches it. An `Elicitation` entry names the Model Context Protocol (MCP) server that asked, with the `mcp__<server>__` prefix, and `ElicitationResult` clears it the same way. An `input` the `Notification` hook wrote has no `tool`, and any completion clears it.
- `agent_id`: optional, present only on an `input` entry a subagent's prompt produced: that subagent's id. Only its own `PostToolUse` or `PostToolUseFailure` for the same tool clears it.
- `source`: optional, `screen` on an entry the sidebar's screen check wrote (section 4). The hooks and the pi extension write no `source`; the screen check's raise case keys on it.

Every writer replaces the file atomically (a temp file, then a rename), so a reader never sees a partial one.

### Freshness

`internal/state/file.go` has one threshold: `freshness = 10 * time.Second`, behind `IsFresh`. A state file younger than that is **fresh**. A fresh file also outranks a program status record (section 1): the hooks saw an event the record may not reflect yet. Past the threshold the record, when kitty lists one, decides; the rules below apply without one.

- `ks list` trusts a fresh file outright, whatever it says. An older file is ignored and the pane text is classified instead (section 4), with one exception. A stale `error` keeps printing `error` unless the pane text shows Claude working, so `ks list` and the sidebar agree on a failed turn.
- The sidebar lets a fresh `input`, `error` or `working` win over the title glyph. Past 10 s the glyph leads, with one exception: an `input` or `error` entry counts whatever its age, until a later hook replaces it. That keeps an unanswered prompt or a failed turn visible. Only a working title glyph outranks a stale one. A stale `input` has one more way out. The session's own sidebar re-reads the screen (section 4) and, once the dialog is gone, rewrites the file to `idle` at the prompt box or `working` mid-turn. A prompt you dismissed clears that way. Its full order is in [Sidebar guide](tui.md#states).

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
| `done` | `done` | The turn finished and the window has not been looked at since. On a seen tab (on screen in the active kitty window) it reads `idle` |
| `error` | `error` | The program reported a failure and stopped |
| `idle` | `idle` | |

The sidebar and `ks list` share this in `launcher.ReportedState`, applied right after the stopped check for every kind (claude, pi and shell). The order: a fresh state file (younger than the 10 s threshold) hands over to the kind's own rules, because the hooks saw an event the record may not reflect yet. Otherwise the record, when kitty lists one, decides. Otherwise the kind's own rules of [Sidebar guide](tui.md#states) apply. The protocol has no `waiting`; that value only ever comes from the `SessionStart` hook or the classifier.

### Lifetime

kitty drops records on its own; ks never writes or clears one.

- `done` and `error` go on the next focus-in or keypress in the window, not on focus-out. ks reads `done` as `idle` while its tab is seen (on screen in the active kitty window), so a result you looked at without typing shows as `done` again once you switch away, until the next visit. `viewed_at` plays no part.
- `working`, `blocked` and `idle` go at a new shell prompt and on a full terminal reset. The prompt is the OSC 133 mark, so only a window running a shell with integration prints one; a claude or pi window has no prompt and keeps its last record until the program replaces it.
- A window's records vanish with the window. A crashed agent leaves nothing behind, and the row shows `stopped` through the window check anyway.

### What the hooks still do

Program status replaces the state file as the badge's source, not the hooks. `SessionStart` is what gives a record its Claude session id and transcript path for `--resume`, and `SessionEnd` what marks a `/exit` `stopped`; neither has a protocol counterpart. The `waiting 12m` age on an `input` row also comes from the hooks' state file (they write `input` for the same prompt), so without them it reads zero. And on a kitty without the protocol the state file is still where the badge comes from. Every ks-launched claude carries the hooks, so there is nothing to keep in place.

For pi, the records kitty passes take precedence over the file the ks pi extension writes. The extension is unchanged and remains the source on an older kitty (see [pi](pi.md)).

## 2. Claude Code hooks

Claude Code fires [hook events](https://docs.claude.com/en/docs/claude-code/hooks) at specific points in its lifecycle. `ks` wires twelve of them to a hidden `ks _hook` handler, which writes the state file and keeps the session record's lifecycle fields current. The state file is the badge's source wherever kitty passes no program status; the record fields are needed everywhere.

### How the hooks reach claude

The launcher injects them. Every claude a ks session starts (`ks new`, `ks open`, `ks tmp`, a resume on attach, the sidebar) gets one `--settings <inline JSON>` argument carrying the ks matcher groups for the twelve events below. The command in every group is the running ks binary followed by ` _hook`, with a `timeout` of 5. The path uses `~/` for your home directory when it needs no shell quoting; a path with a space or a metacharacter is single-quoted and stays absolute. Claude Code runs an identical command string once, so a leftover entry from an older version that matches the injected one does not fire twice. When `input` is in the `notify` config list the same JSON also carries `"preferredNotifChannel":"notifications_disabled"`, the setting that mutes Claude's own banner for a waiting prompt (see [Sidebar guide](tui.md#notifications)). Claude Code loads the inline settings beside your own `~/.claude/settings.json`, so the hooks you keep there keep running.

ks writes nothing to `~/.claude/settings.json`. Earlier versions did write there: `ks hooks install` put the matcher groups into the file. A settings manager that regenerates `~/.claude/settings.json` strips what it does not know about, and the live file ended up with half the events missing. Injecting at launch sidesteps that, and it means every ks-launched claude has the hooks whatever the file says. pi sessions are untouched (pi has its own extension, see [pi](pi.md)), shell sessions get nothing, and `ks import --to cmux` injects nothing either: no ks sidebar watches a cmux workspace.

### Leftovers from older versions

```bash
ks hooks status     # hooks injected at launch, plus any leftover entries
ks hooks uninstall  # remove the ks groups an older version wrote to ~/.claude/settings.json
ks hooks install    # retired: says there is nothing to install, removes leftovers
```

`ks hooks status` prints `hooks injected at launch`. When `~/.claude/settings.json` still holds ks groups from an older version it continues with `; leftover global entries for <events> in <path> (ks hooks uninstall removes them)`. The sidebar's `hooks status` menu entry shows the brief form: `hooks injected at launch`, or `hooks injected at launch; N leftover global entries (ks hooks uninstall)`. A leftover whose command string differs from the injected one (an older binary path, say) fires a second time beside the injected hooks, harmless but wasteful, so `ks hooks uninstall` removes them. With nothing to remove it prints `no leftover ks hooks in <path>` and leaves the file untouched. `ks hooks install` does the same after printing `ks hooks are injected at launch; nothing to install`, then `removed leftover ks hooks from <path>` when it found any. The removal takes any matcher whose command resolves to `ks _hook`, whether written with `~/` or an absolute path. Every field of other tools' entries survives, including ones ks does not model such as `timeout` and `async`. An event whose value is not an array is left alone.

Every ks handler carries `"timeout": 5`, because `PermissionRequest` runs synchronously inside Claude's permission flow and the default hook timeout is 600 s.

### Event → state map

| Event | Matcher | State written | Extra effect |
|---|---|---|---|
| `UserPromptSubmit` | *(empty)* | `working` | Fires when you send a message, before any tool call, so a stale `input` clears on your next message even when you dismissed the prompt |
| `PreToolUse` | `.*` | `working` | `input` instead when the tool is `AskUserQuestion` or `ExitPlanMode`. Both block on you, and under bypass-permissions mode neither fires `PermissionRequest` nor `Notification` |
| `PostToolUse` | `.*` | `working` | Replaces an `input` only when the entry's `tool` is the one that finished, so in a parallel batch a Read finishing leaves the Bash prompt still on screen alone. An `input` from `Notification` names no tool, and any completion clears it |
| `PostToolUseFailure` | `.*` | `working` | Same as `PostToolUse`, for a tool that failed |
| `PermissionRequest` | *(empty)* | `input` | Fires the moment Claude is about to show a permission dialog, so the row turns `input` at once. The entry names the tool that asked in `tool`, and the subagent in `agent_id` when one asked. The handler prints nothing and exits 0, which leaves Claude's own dialog in place |
| `Elicitation` | *(empty)* | `input` | An MCP server opened an elicitation dialog; the empty matcher covers every server. The entry records the server as its `tool`, with the `mcp__<server>__` prefix, so only a completion of that server's tool or its `ElicitationResult` clears it |
| `ElicitationResult` | *(empty)* | `working` | You answered the elicitation. Treated as a completion like `PostToolUse`: it clears the `input` for the same server and never a subagent's prompt |
| `Stop` | *(empty)* | `idle` | |
| `StopFailure` | *(empty)* | `error` | The turn died on an API error, a rate limit or an auth failure |
| `Notification` | `permission_prompt\|elicitation_dialog` | `input` | The fallback behind `PermissionRequest`: `permission_prompt` only fires once the dialog has waited about six seconds |
| `SessionStart` | *(empty)* | `waiting` | Stores the payload's `session_id` and `transcript_path` on the record as `claude_session_id` / `claude_transcript_path` and sets `status` to `active` |
| `SessionEnd` | `prompt_input_exit\|logout` | *(none)* | Sets `status` to `stopped`, removes the state file, and pokes the session's sidebar (SIGUSR2) to close the tab at once. The handler checks the reason again, so a `clear`, `resume` or `other` that slips through is still ignored |

`Notification` is registered for `permission_prompt` and `elicitation_dialog` only. Claude Code also sends `idle_prompt` after about a minute at an empty prompt. Mapping it to `input` would turn every idle session into a false "waiting on you" and bury the real ones. `done` and `idle` already cover that case. Should another type ever belong here, it is one more alternative in the `Notification` matcher in `internal/hooks/hooks.go` and a case in `stateForEvent` in `internal/cli/hook.go`.

Claude Code fires no hook when you dismiss a prompt with Escape or deny it by hand, and `Stop` does not run on an interrupt. The hooks alone would leave an `input` row set by a prompt you dismissed red until the next event they see: your next message (`UserPromptSubmit`), a `/clear` (`SessionStart`) or an exit (`SessionEnd`).

The screen check of section 4 covers the gap. Once the `input` entry is older than 10 s, the session's own sidebar reads its claude window on each 3 s poll. A dialog still on screen keeps `input`. Otherwise a running turn rewrites the file to `working` and the prompt box at rest rewrites it to `idle`, so the row clears within one poll. On a kitty that passes program status the record takes over after ten seconds instead, and the screen is never read. The dialogs the hooks never see get the same treatment. Those are the folder-trust dialog and a login prompt at startup, which come before any hook fires. The screen check sees them and writes `input`.

`StopFailure` writes `error`: the turn ended on an API error, a rate limit or an auth failure. Without it a failed turn sat at `working` until the title glyph caught up. The sidebar treats the value like `input`: fresh, it wins outright; stale, it stays until a later hook, and only a working title glyph outranks it. `ks list` prints `error` from the file whatever its age, unless the pane text shows Claude working.

Every launch path (`ks new`, `ks open`, `ks tmp`, the sidebar) exports three variables into both of the session's windows: `KS_SESSION_NAME=<name>`, `KS_SESSION_ID=<id>` and `KS_EXE=<path of the ks binary>` (the pi extension runs `$KS_EXE _pi-hook`, so it reaches the build that made the session). The hook finds the record by `KS_SESSION_ID` first (the `id` field, stable across renames) and falls back to `KS_SESSION_NAME` for records written before ids existed. The state file is keyed by the record's *current* name, so a rename made in the sidebar does not strand later hook writes. If `KS_SESSION_NAME` is unset, the hook exits silently, so a leftover global entry from an older version does no harm beyond the duplicate run.

Two more guards keep the record honest:

- **Nested claudes are ignored.** A `claude` started from inside a session (say `claude -p ...` from the Bash tool) inherits `KS_SESSION_NAME`. The hook therefore checks the process tree: the Claude that fired it must be a direct child of the `kitty` process, which is how `kitty @ launch` starts it. Anything else exits 0 without writing. The lookup lives in `internal/procinfo` and only exists on macOS; elsewhere the hook trusts the environment as before.
- **Subagents write state, not the record.** Events carrying an `agent_id` come from an in-process subagent. They write only `input`: a subagent's permission prompt or question blocks you too. The entry carries the subagent's id and the tool that asked, and that subagent's own `PostToolUse` or `PostToolUseFailure` for the same tool clears it to `working`. Every other subagent event writes nothing, so a background subagent cannot overwrite the parent's `input` or `error` and cannot idle the parent when it finishes. `SessionStart`/`SessionEnd` from a subagent leave `claude_session_id` and `status` alone.

### Session ID and status

Four fields on the session record (`~/.config/ks/sessions/<name>.json`) outlive kitty restarts:

- `id`: random, assigned by `ks new`/`ks tmp` (or on the first reopen of an older record) and never changed. It is what `KS_SESSION_ID` carries.
- `claude_session_id`: the `session_id` from the most recent `SessionStart` payload. Every source (`startup`, `resume`, `clear`, `compact`, `fork`) updates it, so after a `/clear` the record points at the new conversation.
- `claude_transcript_path`: the `transcript_path` from the same payload. `ks open` on a session whose claude window is gone runs `claude --resume <id>` only while that file still exists. Records without the field fall back to `~/.claude/projects/<encoded dir>/<id>.jsonl`. Claude Code purges transcripts after its cleanup period, and `--resume` then fails with "No conversation found", so a missing file means `claude --continue` instead.
- `status`: `active` or `stopped`. `SessionEnd` with reason `prompt_input_exit` (the user typed `/exit`) or `logout` writes `stopped`. Reason `other` is what Claude reports when kitty closes the window, and `clear`/`resume` are restarts, so those leave the record `active`. `ks close --keep` and the sidebar's close action also write `stopped` before closing the tab; they close the sidebar with it, so the reap below never sees them. When claude exits on its own, the session's sidebar finds the window gone. It then moves a claude or pi record to the trash, marked `stopped`, once the hook wrote `stopped` or the agent had run for ten seconds. So after an `/exit` the row is gone rather than stopped; see [When the agent exits](tui.md#when-the-agent-exits). Every launch path saves the record as `active` *before* the first kitty call, so `SessionStart` always finds it. Afterwards it writes only the kitty IDs back, so the hook's update is not clobbered. Records written by older `ks` versions have no `status` field and read as `active`.

The state file and the record are updated independently: a failure in one does not skip the other. Record problems (missing, unreadable, unsaveable) are printed to stderr as `ks _hook: <error>` and the hook still exits 0, so Claude's hook run never fails because of `ks`.

### What claude is started with

Simplified, the JSON behind `--settings`:

```json
{
  "preferredNotifChannel": "notifications_disabled",
  "hooks": {
    "UserPromptSubmit": [ /* ... */ ],
    "PreToolUse": [
      {
        "matcher": ".*",
        "hooks": [
          {"type": "command", "command": "~/code/bin/ks _hook", "timeout": 5}
        ]
      }
    ],
    "PostToolUse":        [ /* matcher ".*" */ ],
    "PostToolUseFailure": [ /* matcher ".*" */ ],
    "PermissionRequest":  [ /* ... */ ],
    "Elicitation":        [ /* ... */ ],
    "ElicitationResult":  [ /* ... */ ],
    "Stop":               [ /* ... */ ],
    "StopFailure":        [ /* ... */ ],
    "Notification":       [ /* matcher "permission_prompt|elicitation_dialog" */ ],
    "SessionStart":       [ /* ... */ ],
    "SessionEnd":         [ /* matcher "prompt_input_exit|logout" */ ]
  }
}
```

Every elided group has the same one-handler shape, `timeout` included. `preferredNotifChannel` is there only while `input` is in `notify`. The command names the ks binary that launched the session (`~/` for your home directory unless the path needs quoting), so a build installed under another path is picked up on the next relaunch.

## 3. Background Haiku agent (optional fallback)

Launched by `ks sidebar --agent`, or by the home sidebar when `ks --agent` starts the instance. The agent is a long-running `claude` invocation with a hardened system prompt and a tightly restricted `--allowedTools` list. It loops every five seconds:

1. List session files in `~/.config/ks/sessions/`.
2. For each one with a live kitty window ID, run `kitty @ get-text --match=id:<id>`.
3. Classify the terminal text into `working` / `idle` / `input` / `waiting`.
4. Write the result to `~/.config/ks/state/<name>.json`.

The agent uses the `haiku` model alias. Its allowed tools are just `Bash(kitty @ --to <socket> get-text *)`, `Bash(ls ...)`, `Bash(sleep *)`, plus read/write access to the session and state directories. The system prompt itself forbids launchd/cron/plist/scripts. When that sidebar exits, `ks` kills the agent's process group so nothing lingers.

Use the agent as a belt-and-braces setup alongside the hooks; every ks-launched claude already carries them.

## 4. Title glyph and terminal text (always-on)

The sidebar reads the claude window's title from the `kitty @ ls` snapshot it already takes. Claude Code prefixes that title with a glyph: a spinner (`◐◓◑◒`, `✢✶✻✽`, or a braille pattern) while it works and `✳` at the prompt. `internal/claude.ParseTitle` turns that into `working` or `idle`; the rest of the title is the row's second line. How the sidebar combines it with the state file is in [Sidebar guide](tui.md#states).

### The screen classifier

`internal/claude.ParseScreen` reads the bottom of the claude window's text and reports three flags. The dialog rules look at the last 24 non-empty lines, the running-turn rules at the last 12:

- **A dialog is on screen** (a blocker). The rules are ported from herdr's Claude manifest (Apache 2.0), plus one of ks's own for the folder-trust dialog. Any of these matches: `esc to cancel` together with `enter to confirm`, or `enter to select` plus an arrow-key hint (the words, or `↑/↓`); `do you want to proceed?` with a numbered option line in the `❯ 1. Yes` style; `MCP server "…" requests your input` with Accept/Decline and `esc to cancel`; `review your answers` (the AskUserQuestion review); `skip interview and plan immediately` (the plan interview); `waiting for permission`; `do you want to allow this connection?`; and `trust the files` with `enter to confirm` (the trust dialog).
- **The prompt box is at rest**: a `❯` line between the last two horizontal rules, with no dialog footer inside the box.
- **A turn is running**: an `esc to interrupt` line, or a spinner-glyph line ending in `…`.

The transcript viewer and the model picker match none of these, so opening either never changes a state. A Claude UI change that no rule matches fails safe the same way: the flags stay off and the callers below keep today's behaviour.

Both callers read the window with `kitty @ get-text --extent all`. The default extent returns what the viewport shows. Once you have scrolled up that is a slice of the scrollback, not the live screen (probed on kitty 0.48.2, 2026-10-16). `--extent all` always ends at the live bottom. Before classifying, both cut that text to the window's own row count, the `lines` field of `kitty @ ls`. So rows left in the scrollback above a short screen, after a `/clear` say, cannot pose as a running turn or a dialog.

### `ks list`

`ks list` runs `internal/claude.DetectState`, built on the three flags: a dialog is `input`, a running turn `working`, the prompt box at rest `idle`, the Claude Code welcome screen `waiting`, and anything else `waiting`.

### The sidebar's screen check

The session's own sidebar, and only it, re-reads its own claude window after the rules above, in two cases. Neither runs while the window carries a program status record (section 1).

1. The clear, on every 3 s poll. The row is `input` and the state file is older than 10 s. A dialog still on screen keeps `input`, whatever else the screen shows. Otherwise a running turn rewrites the file to `working`, and the prompt box at rest rewrites it to `idle`. The row then reads `idle`, or `done` while the tab is not on screen or the kitty window is not the active one, since the file's `idle` is newer than `viewed_at`. A window that cannot be read leaves the file alone.
2. The raise, reading at most once every 10 s. The row has no state file at all, a stale `waiting`, or a state file the screen check itself wrote last (`"source": "screen"`; the hooks write no source). The first two are a dialog at startup: the hooks miss it because the folder-trust dialog comes before any hook runs, and a login prompt reports nothing. The third keeps the backstop open for a session whose injected hooks never fire, with `disableAllHooks: true` in your settings for example, still at most one read every 10 s. A visible dialog writes `input`.

Before it writes, the sidebar re-reads the state file and skips the correction when a hook wrote in the meantime. Because the session's own sidebar writes the shared state file, every other sidebar and `ks list` follow without reading the screen themselves. `error` is never touched by the screen check.

This is still text matching. Claude's UI changes, and a rule can stop matching. The hooks and program status are the accurate sources; this is the backstop behind them.

## Choosing between hooks and the agent

|  | Hooks (injected at launch) | Agent (`ks --agent`) |
|---|---|---|
| Accuracy | High: driven by Claude Code internals | Medium: driven by terminal classification |
| Latency | Immediate (hook fires synchronously) | Up to ~5 seconds |
| Cost | Zero extra model calls | Ongoing Haiku usage while `ks` runs |
| Lifespan | The whole run of every ks-launched claude | Only while the sidebar that started it runs |
| Per-session opt-out | None; every ks-launched claude gets them | Per-session; skips windows without a kitty ID |

The hooks come with every session and need no setup. The agent is a second reader of the screen beside them. On a kitty that passes program status, neither decides the badge for a claude or pi that speaks the protocol. The hooks still carry the session id and the stopped status, which is why every session gets them.

## Inspecting state

```bash
ls ~/.config/ks/state/
cat ~/.config/ks/state/<name>.json
kitty @ --to unix:$HOME/.config/ks/kitty.sock ls   # program_status under each window
```

`ks list` prints the current state alongside the name, the agent kind and the directory. Both go through `launcher.ReportedState`, so with a record and no fresh state file they agree. Without one the sidebar's dot is computed separately, so the two can differ. It adds `done`, reads the title glyph instead of the pane text, and keeps `input` until a later hook replaces it or its own screen check clears it. It also draws a row's name in the accent colour while the row is unread: any state file but `waiting` newer than the record's `viewed_at` while the tab is not seen (on screen in the active kitty window). `done` is the unread case where the turn also finished (see [Sidebar guide](tui.md#unread-rows)). A stale `error` is the exception: both keep it until Claude is seen working, in the pane text or the title, so a failed turn reads the same in both. For a pi session both then read the state file alone; for a shell both say `idle`.

## Cleaning state

- `ks close <name>` (with or without `--keep`) and the sidebar's close and delete actions all go through one helper, `launcher.Close`. It removes the state file, closes the session's live tabs, and then either marks the record `stopped` or moves it to the trash.
- The `SessionEnd` hook removes the state file when it marks a record `stopped`, and pokes the sidebar to close the tab at once.
- Every relaunch (a new session, or a resume of a session whose window is gone) removes the old state file before the launch; the `SessionStart` hook writes a fresh one. So an attach never shows a stale `input` from before, and the sidebar never posts a banner for it.
- Stale state for sessions that no longer exist is harmless. `ks list` and the sidebar only look up state for sessions that are present in the session store. A record whose `status` is `stopped` shows `stopped` without consulting kitty or the state file.
