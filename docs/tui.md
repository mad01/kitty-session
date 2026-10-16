# Sidebar guide

The sidebar is `ks sidebar`, a fixed-width [Bubble Tea](https://github.com/charmbracelet/bubbletea) view that lists every agent (session) with its state. The ks instance runs one in its home tab and one on the left of every session tab (`ks sidebar --session-id <id>`). The tab bar is hidden, so the sidebar is the tab list. Every session action the subcommands expose is available from here too.

## Launch

```bash
ks                    # attach; the instance opens with a sidebar in its home tab
ks sidebar            # the sidebar in the current terminal, by hand
ks sidebar --agent    # same, plus the background Haiku state monitor
ks _sidebar-demo      # the sidebar on fake agents; no kitty needed
```

The home tab exists only while no session tab does: the first session tab closes it (unless its sidebar runs `--agent`, which would take the state monitor with it), and closing the last session tab brings it back when no other tab would be left, so the instance stays up. ks knows the home tab by the `KS_HOME` user variable its sidebar sets on its own window. A tab you open yourself in the instance (cmd+t) carries neither that nor a session tag, so ks leaves it alone, and it keeps the instance up on its own. With the tab bar hidden, a kitty `goto_tab 1` therefore lands on a session rather than on the home tab's blank right half. In the home tab the sidebar shows `no agent in this tab · enter to focus · n for new` under its header.

Run by hand, `ks sidebar` needs the instance to be up and exits with `ks instance not running` otherwise. `--agent` is a fallback for when the Claude Code hooks are not installed; see [Hooks and state detection](hooks-and-state.md). `_sidebar-demo` is hidden and takes `--width` and `--session` to tune the preview.

## Rows

The frame is `sidebar_width` columns wide (default 36, see [Configuration](configuration.md)) and as tall as the window. Its border is light green while the sidebar window has the keyboard and muted gray otherwise, so the frame tells you which side of the tab has focus. Under the `agents … tab order` header each agent takes two lines:

- A dot for the state, then the name. A session whose agent is not claude carries a badge after the name: `pi` for a pi session, `sh` for a plain shell. The cursor row has a highlight; the row of the tab the sidebar sits in carries a `▌` marker and its own background. The name draws in the accent colour while the row is unread (see [Unread rows](#unread-rows)).
- Claude's tab title minus its state glyph (`Plan the merge`), or the session directory with `$HOME` shortened to `~` when Claude has not set one. A pi or shell row shows its window's title the same way, or the directory.

The footer has two labels, `new` and `menu`, and the line above it shows the result of the last action or its error until the next key. The list refreshes every three seconds and after every action; the `working` and `input` dots pulse.

## States

| Dot | State | Meaning |
|---|---|---|
| `●` pulsing red | `input` | The agent is waiting on you: a permission prompt, a question, the folder-trust dialog or a login prompt at startup. It stays until you answer; after a minute the title line leads with how long (`waiting 12m`) |
| `✗` in the error colour | `error` | The agent reported a failure and stopped. A program status record says so, or the `StopFailure` hook saw a turn die on an API error, a rate limit or an auth failure |
| `●` | `done` | The agent finished a turn and you have not looked at the tab since |
| `●` pulsing amber | `working` | Claude is processing |
| `○` | `idle` | Claude is at its prompt and the result has been seen |
| `·` | `stopped` | The record is stopped, or no claude window carries the session's tag |

Each row's state comes from one `kitty @ ls` snapshot plus the session's state file. Three rules come first, for every kind:

1. Record `stopped`, or no agent window tagged with the session's id: `stopped`.
2. State file less than 10 s old: the kind's own rules below. The hooks saw an event the record may not reflect yet.
3. The agent window carries a program status record, an Operating System Command 7501 report from the program (see [Hooks and state detection](hooks-and-state.md#1-program-status-osc-7501-preferred)): `blocked` is `input`; `working`, `error` and `idle` are themselves; `done` is `done`, or `idle` on the tab you are looking at. The record is the program's own word. kitty 0.49.2 and older pass no records.

Without a record, or with a fresh state file, the rules depend on the agent kind. A shell row is `idle`: nothing else reports a shell's state. A pi row has no title glyph to read, so its state file decides alone, whatever its age; the ks pi extension writes it (see [pi](pi.md)). `input` and `working` show as they say. `idle` is `done` when the file is newer than the record's `viewed_at` and `idle` otherwise. Anything else (`waiting`, no file) is `idle`. For a claude row, first match wins:

4. State file says `input` and is less than 10 s old: `input`.
5. State file says `error` and is less than 10 s old: `error`.
6. State file says `working` and is less than 10 s old: `working`. A fresh working file outranks the `✳` title below, because `✳` is also one of Claude's spinner frames, so a snapshot mid-turn can catch it.
7. Claude's title starts with a spinner glyph: `working`.
8. State file says `input`, however old: `input`. The hooks write `input` when a prompt appears. No hook replaces it until the next event: your next message, a tool call, a finished tool, the end of the turn, the session ending. So an unanswered prompt keeps its row red for hours. The spinner title of rule 7 is the one thing that outranks it here. It means you have answered and Claude is mid-turn, which the hooks do not report until the tool you approved finishes or the next one starts. A prompt you dismissed is caught by the screen check below instead, which rewrites the stale file once the dialog is gone.
9. State file says `error`, however old: `error`. The `StopFailure` hook writes it when a turn dies on an API error, a rate limit or an auth failure. It is sticky like `input`: the next hook or the spinner title of rule 7 clears it.
10. Claude's title starts with the idle glyph `✳`: `done` when the state file says `idle` with an `updated_at` newer than the record's `viewed_at`, else `idle`.
11. No glyph: the state file's `working` as is; `idle`, `waiting`, or no file at all: `idle`.

A tab counts as seen while it is on screen and the kitty window is the active one (the OS window's `is_focused` in `kitty @ ls`). `viewed_at` is stamped the moment the session's tab becomes seen, or is opened from any sidebar, and once more when it stops being seen. So what you watched until the switch counts as seen. In between, its own sidebar refreshes the stamp at most every 10 s. A turn that finishes while you are in another tab, or in another app with the tab showing, reads `done` until you come back. It drops to `idle` on the next refresh after that. A `done` record needs none of that, with one catch: kitty drops it on the next focus-in or keypress in the window, not on focus-out. The sidebar reads it as `idle` while its tab is seen, but a result you looked at without pressing a key shows as `done` again once you switch to another tab, until the next visit. Without the hooks the only state file is the one the screen check below writes. On a kitty that passes program status, `input`, `done` and `error` still come from the record. The `waiting 12m` age on an `input` row does not: it is read from the state file, so it stays at zero. On an older kitty only the title glyph is left, which gives `working` and `idle`.

### Unread rows

A row is unread when the session's state changed since you last saw its tab. That is, the state file's stamp is newer than the record's `viewed_at`, and the tab is not seen right now (on screen in the active kitty window). Any change counts, a running turn included, so it works like kitty's own tab activity marker. A `waiting` file does not count: a launch is not activity. `done` keeps its meaning inside it: finished and not looked at. While a row is unread its name draws in the accent colour; the state dot, the title line and the sort order are unchanged. The row is read the moment its tab is seen or is opened from any sidebar. The state file is written by the hooks, the screen check and the pi extension. When none of them writes one, nothing marks a row unread.

### The screen check

Rules 4 to 11 never read the window's text. The session's own sidebar adds one step after them, for its own row only. It reads its claude window with `kitty @ get-text --extent all`, cut to the window's own rows, and runs the screen classifier of [Hooks and state detection](hooks-and-state.md#4-title-glyph-and-terminal-text-always-on), in two cases. Neither runs while the window carries a program status record.

- The row is `input` and the state file is older than 10 s, checked on every 3 s refresh. A dialog still on screen keeps `input`. Otherwise a running turn rewrites the file to `working`, and the prompt box at rest rewrites it to `idle`. The row then reads `idle`, or `done` on a tab you are not looking at, since the file's `idle` is newer than `viewed_at`. A window that cannot be read leaves the file alone.
- The row has no state file, or a stale `waiting`, checked at most once every 10 s. A dialog on screen writes `input`. This is how the folder-trust dialog and a login prompt at startup get a red dot: no hook has run yet. While the hooks are not installed, any dialog at any time counts, so ks works without `ks hooks install` at all.

Before it writes, the sidebar re-reads the state file and skips the correction when a hook wrote in the meantime. The file is shared, so the home sidebar, the other sessions' sidebars and `ks list` follow without reading the screen. `error` is never touched. A Claude UI change that matches no rule changes nothing: the row keeps the state the rules above gave it.

### Sort

Rows follow the tab order of the ks instance: the top row is the first tab, which kitty's default macOS keys reach with `cmd+1`, the next is `cmd+2`, and so on. State never moves a row, so the number you see is the number you press, and the sidebar's own `1`-`9` jump keys agree with kitty's. Stopped sessions have no tab and sit at the bottom, by name. Bare `ks`, and any command that has to start the instance, opens tabs in the order `ks move` last left them, then the rest in the order the sessions were created, before adding anything new, so the numbering survives a restart. `ks move <name> <where>` (or `ks move <where>` from inside a session) reorders the tabs; see the [command reference](commands.md#ks-move-name-where). A stopped session you reopen gets a fresh tab at the bottom. The cursor stays on the same agent across refreshes.

## Notifications

The session's own sidebar, and only it, posts a desktop notification for its own row. It writes the notification to its own terminal through kitty's desktop notification protocol, Operating System Command (OSC) 99 (see the [kitty docs](https://sw.kovidgoyal.net/kitty/desktop-notifications/)). The sidebar sits in the session's tab, so the request carries kitty's `o=invisible` flag. kitty then shows the banner only while that tab is not seen: off screen, or on screen in a kitty window that is not the active one. A prompt in the tab you are looking at posts nothing. One that lands in the visible tab while you are in another app does, and the row stays unread until you come back. Clicking the banner focuses the sidebar window, which brings the tab to the front.

Which transitions post is the `notify` list in `config.yaml` (see [Configuration](configuration.md#notify)); the default is `[input]`.

- `input`: the row enters `input`. The banner reads `<name> needs input`, its body is the row's second line (Claude's title or the session directory), and it plays kitty's `question` sound. Leaving `input` withdraws it.
- `done`: the row enters `done`. The banner reads `<name> finished`, with the same body and the `info` sound. Leaving `done` withdraws it.

A sidebar that starts while its session already waits posts at once. A relaunch removes the old state file first and the `SessionStart` hook writes a fresh one, so an attach never raises a banner for a stale `input` from before. The home tab's sidebar posts nothing. There is no delay before posting: kitty's visibility gate is the suppression, and a prompt answered within one poll just has its banner withdrawn. There is no per-session opt-out and no `error` notification.

A banner is withdrawn when the row leaves the state. It is also withdrawn when you close or delete the session from the sidebar, and when the session's sidebar closes its own tab after claude exits. A session closed with `ks close` from a shell, and every session on `ks quit`, keeps its banner until you dismiss it. Clicking such a banner does nothing: the window it pointed at is gone.

The badge and the banner come from the same resolved state. Whatever source gave the row its `input` (a hook, a program status record, the screen check) posts the banner too; see [Hooks and state detection](hooks-and-state.md).

Claude Code has a channel of its own: it posts a desktop notification in kitty when a permission prompt waits and you seem away (`preferredNotifChannel: auto`). To avoid two banners for one prompt, `ks` starts claude with `--settings '{"preferredNotifChannel":"notifications_disabled"}'` whenever `input` is in `notify`. Without `input` in the list (`[]` or `[done]`) Claude's channel is left alone, by design: it is then the only signal for a waiting prompt.

`ks _sidebar-demo` turns notifications on for its demo row, so in kitty it previews the banners. Only kitty implements OSC 99 here; other terminals ignore the sequence.

## Keys

| Key | Action |
|---|---|
| `j` / `↓`, `k` / `↑` | Move the cursor |
| `1`-`9` | Jump to that row and focus it |
| `enter` | Focus the agent under the cursor; a tab that is gone is recreated |
| `l`, `tab`, `q` | Hand the keyboard to this tab's claude window |
| `n` | New agent: the repo picker, then the kind (claude, pi or shell) |
| `r` | Rename the agent under the cursor |
| `c` | Close its tab, keeping the record (asks first) |
| `d` | Delete it: close the tab and trash the record (asks first) |
| `u` | Restore a trashed session |
| `/` | Filter rows by name; `enter` keeps the filter, `esc` clears it |
| `m` | Open the menu |
| `?` | Show the keys popup; `esc`, `?` or `q` closes it |

`ctrl+c` does nothing: the sidebar never exits on its own. Its tab closes once its claude is gone, see [When claude exits](#when-claude-exits). To end ks use the menu's `quit ks` or `ks quit`.

### Between the sidebar and claude

`l`, `tab`, `q` or `enter` move the keyboard from the sidebar to a claude window. The way back is your own kitty window keys: the instance maps no chords of its own, so whatever moves between windows in your kitty does so here too. In this dotfiles config that is `cmd+]` for the next window and `cmd+[` for the previous one. `ctrl+b` and `ctrl+w` reach Claude unchanged (background a task, delete a word). To add chords, put `map` lines in `kitty_overrides`, for example `map ctrl+b>s neighboring_window left`.

## Mouse

Left click selects and focuses a row, opens the picker from the `new` label, opens the menu from the `menu` label, and runs the menu entry under the pointer. A click outside the menu closes it.

## New agent (`n`)

The picker shows `tmp` first, then every git repository under the configured `dirs`, scanned while you type. Typing filters the list (fuzzy); `↑`/`↓` or `ctrl+p`/`ctrl+n` move, `enter` picks, `esc` goes back. `tmp` creates a scratch directory under `tmpdir`, like `ks tmp`.

The session name is the directory's base name plus the checked-out git branch, lower-cased with other characters collapsed to hyphens (`kitty-session-main`). When a directory has no name to offer, a name prompt opens first; `enter` takes the name on, `esc` returns to the picker.

The last step is the kind: a small popup over the name screen listing `claude`, `pi` and `shell`, with the cursor on `claude`. `j`/`k` or the arrows move, `enter` creates the session with that kind (so `enter` alone gives you claude, as before), `esc` goes back to the name prompt with the chosen kind remembered. When the name turns out to be taken, the name prompt reopens with the reason; `enter` there returns to the kind popup.

## Rename (`r`)

The cursor row turns into an input pre-filled with the current name. `enter` saves, `esc` cancels. The record, its state file and the tab title are renamed together.

## Close and delete (`c`, `d`)

Both pop a confirmation with the agent's name. `y` or `enter` confirms, `n` or `esc` cancels. Close removes the tab and marks the record `stopped`; `enter` on the row later brings it back. Delete also moves the record to `~/.config/ks/sessions/trash/`.

## When claude exits

A session tab closes on its own once its claude window is gone, whether claude ended with `/exit`, crashed, or was killed. The tab's sidebar checks its claude window on every refresh. Once it has seen the window and then finds it missing, it closes its own tab the way `c` does. The home tab is recreated first when it was the last session tab. A claude that never shows up within 30 seconds of the sidebar's first refresh (bad flags, a missing binary) closes the tab as well. Another window of the session still in the tab, a shell split for one, holds the tab open until it exits too.

The record is not touched. `/exit` is marked `stopped` by the SessionEnd hook, and the row sits at the bottom until `enter` reopens it. A crash leaves the record `active`, so the next `ks` brings the session back. The home tab's sidebar never closes anything.

## Trash and restore (`u`)

A popup lists the trashed sessions; `j`/`k` move, `enter` restores, `esc` cancels. A restored session comes back `stopped`. `enter` on its row recreates the tab, with `claude --resume` when its transcript is still there and `--continue` otherwise.

## Menu (`m`)

A popup above the footer: `new agent`, `rename`, `close (keep)`, `delete`, `restore`, `shell split`, `hooks status`, `keys`, `quit ks`. `j`/`k` move, `enter` runs the entry, `esc`, `m` or `q` close it.

- `shell split` opens a shell below this tab's claude window, in the session directory, taking roughly a third of the height. The home tab has no agent, so there the entry reports that instead.
- `hooks status` says whether the ten Claude Code hook events are registered in `~/.claude/settings.json`, or which are missing. An event missing from an older install is added by `ks hooks install`.
- `quit ks` closes every window of the instance, like `ks quit`. Records stay active and come back on the next `ks`.

## Width

A kitty window resize keeps the split's proportions, not the sidebar's cell count. The sidebar notices its own new width and asks kitty to resize it back to `sidebar_width`; the frame itself never draws wider than the window.
