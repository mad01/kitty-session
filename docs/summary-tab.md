# Summary tab

The summary tab is an opt-in third kitty tab that runs a Haiku-powered agent to read and summarize the session's Claude Code pane. It's useful when Claude is off doing a long refactor and you want a structured view of what it's currently working on without staring at the raw output.

## Requirements

Two config keys, both in `~/.config/ks/config.yaml`:

```yaml
layout: tab
summary: true
```

The summary tab only launches when **both** are set. With `layout: split` there's nowhere sensible to place a third pane, so the `summary` flag is ignored.

You also need `claude` on `PATH` — the summary agent is just `claude` invoked with a tight allow-list.

## How it launches

Whenever `ks new` or `ks open` creates or recreates a session under the right config, `summary.LaunchTab` runs:

```
kitty @ launch --type=tab --match=id:<claude-window>
  --cwd=<session-dir>
  -- claude
     --model claude-haiku-4-5-20251001
     --allowedTools "Bash(kitty @ get-text --match=id:<claude-window>)"
     --system-prompt <prompt>
```

The new tab is titled `summary`. Its window ID is stored in the session file under `kitty_summary_window_id` so `ks close` and `ks delete` can tear it down.

After launching, `ks` waits three seconds (for `claude` to finish starting up) and sends `refresh\n` to kick off the first summary.

## What it shows

The system prompt asks the agent to produce this structure whenever it receives `refresh`:

```
---
**Status:** working | idle | waiting for input | planning
**Task:** One-line description of the overall goal
**Current action:** What Claude is doing right now
**Files touched:** List of files read, edited, or created this session
**Tools in use:** Which tools Claude is actively using
**Errors/blockers:** Any errors, test failures, or permission prompts (or "none")
**Progress:** starting | midway | wrapping up | blocked
**Context:** 2-3 sentences describing what's happening
---
```

If the agent can see conversation history, it also appends a **Recent activity** section with the last 3–5 actions.

## When it refreshes

Three triggers send `refresh\n` to the summary window:

1. **On launch** — three seconds after creation, to produce the first summary.
2. **From Claude Code hooks** — the `_hook` handler sends `refresh\n` on the `Stop` event and on `PreToolUse` events where the tool is `EnterPlanMode` or `ExitPlanMode`. So the summary updates every time Claude finishes or enters/exits plan mode.
3. **From the TUI** — a five-minute ticker iterates every session and sends `refresh\n` to any that have a summary tab, so summaries stay fresh even for sessions that don't use hooks.

The summary agent is instructed not to loop or poll on its own. It waits for `refresh` messages.

## Sandbox

The summary agent has exactly one allowed tool:

```
Bash(kitty @ get-text --match=id:<main-claude-window>)
```

No `Read`, `Write`, `Edit`, `Grep`, `Glob`, `Agent`, and no other `Bash` variants. The system prompt reinforces this. The only thing the agent can do is read the terminal text of the session's Claude Code pane and print a summary to its own terminal.

## Closing

The summary tab closes automatically when the parent session is closed or deleted (`ks close`, `ks delete`, or `c`/`d` from the TUI). If you close the summary tab manually from kitty, `ks` tolerates it — the stored window ID just goes stale and future close calls no-op.

## Model cost

Every `refresh` runs a Haiku call against the full terminal text. The cadence (hook-driven + 5-minute ticker) is deliberately conservative, but if you run many long sessions for long stretches this still costs real money. Leave `summary: false` if that's a concern.
