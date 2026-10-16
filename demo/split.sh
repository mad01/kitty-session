#!/usr/bin/env zsh
#
# Record images/split.webp: a real ks tab, the sidebar on the left and claude
# on the right, from a throwaway ks instance. `make demo-split` runs it.
#
# The instance lives under a scratch HOME. ks keeps everything under
# ~/.config/ks, so a different HOME is a different instance and socket. The
# scratch HOME mirrors the real one through symlinks: Claude Code's config,
# ~/Library for the keychain that holds its login, ~/.config apart from ks,
# ~/code/bin and the dotfiles checkout for the hooks in the user's settings.
# KITTY_CONFIG_DIRECTORY points at the real kitty config, so the window has
# the user's theme and font.
#
# The script creates three sessions. One claude is asked to open a question
# dialog, which turns its row red. One is a plain shell. The third claude has
# its folder-trust dialog accepted on camera and then answers one prompt.
#
# Frames come from `screencapture -l <window id>`, kitty's platform_window_id,
# which needs Screen Recording permission for kitty. ffmpeg turns them into a
# lossless animated WebP with the captured timing. WebP rather than GIF: the
# pulsing dot and Claude's spinner sit in opposite corners of the window. A
# GIF rewrites most of the frame on every tick and lands near 7 MB; WebP's
# inter-frame coding keeps the same frames under 1 MB.
#
# Needs: kitty, claude, jq, ffmpeg on PATH, and ./ks built.
set -euo pipefail
zmodload zsh/datetime

root=$(cd "$(dirname "$0")/.." && pwd)
ks="$root/ks"
real_home="$HOME"
scratch="${KS_DEMO_HOME:-$real_home/.tmp/ks-demo-home}"
src="$scratch/code/src/github.com/mad01"
frames="$scratch/frames-$(date +%Y%m%d-%H%M%S)"
sock="unix:$scratch/.config/ks/kitty.sock"
out="$root/images/split.webp"
export KITTY_CONFIG_DIRECTORY="${KITTY_CONFIG_DIRECTORY:-$real_home/.config/kitty}"

prompt_main='What does this repository do? Read only README.md and answer in one sentence.'
prompt_bg='Use the AskUserQuestion tool to ask me whether I prefer tabs or spaces. Do not answer yourself.'

at() { kitty @ --to "$sock" "$@"; }
log() { print -u2 "split: $*"; }

setup_home() {
    mkdir -p "$scratch/.config/ks" "$src" "$scratch/code" "$frames" "$scratch/.old"
    # Library carries the login keychain; without it claude cannot read its
    # OAuth token and every turn fails. A real directory a previous launch
    # created in the scratch home is moved aside so the link can take its place.
    for f in .claude .claude.json .gitconfig .zshrc .zprofile .zshenv .local .cache Library; do
        [[ -e "$real_home/$f" ]] || continue
        [[ -d "$scratch/$f" && ! -L "$scratch/$f" ]] && mv "$scratch/$f" "$scratch/.old/$f-$(date +%s)"
        ln -sfn "$real_home/$f" "$scratch/$f"
    done
    for e in "$real_home"/.config/*(N); do
        [[ $(basename "$e") == ks ]] && continue
        ln -sfn "$e" "$scratch/.config/$(basename "$e")"
    done
    ln -sfn "$real_home/code/bin" "$scratch/code/bin"
    ln -sfn "$real_home/code/src/github.com/mad01/dotfiles" "$src/dotfiles"
    [[ -d "$src/kitty-session/.git" ]] || git clone -q "$root" "$src/kitty-session"
    for d in brain scratch; do
        [[ -d "$src/$d/.git" ]] || { mkdir -p "$src/$d" && git -C "$src/$d" init -q; }
    done
    cat > "$scratch/.config/ks/config.yaml" <<'YAML'
dirs:
  - ~/code/src/github.com
sidebar_width: 36
notify: [input]
kitty_overrides:
  - initial_window_width=130c
  - initial_window_height=36c
  - remember_window_size=no
YAML
    # Records from an earlier run would be resumed by the first ks new.
    find "$scratch/.config/ks/sessions" "$scratch/.config/ks/state" -type f -delete 2>/dev/null || true
}

# window reads the agent window id of a session from its record.
window() { jq -r .kitty_window_id "$scratch/.config/ks/sessions/$1.json"; }
sidebar() { jq -r .kitty_sidebar_window_id "$scratch/.config/ks/sessions/$1.json"; }

# await_screen waits until the window's screen contains the pattern.
await_screen() {
    local id=$1 pattern=$2 i
    for i in {1..120}; do
        if at get-text --match "id:$id" 2>/dev/null | grep -q -- "$pattern"; then return 0; fi
        sleep 0.5
    done
    log "window $id never showed $pattern"; return 1
}

# await_prompt waits until claude's prompt box is on the window's screen:
# the status line under it, which no dialog shows.
await_prompt() { await_screen "$1" 'shift+tab to cycle'; }

# await_state waits until the session's state file reports the state.
await_state() {
    local name=$1 want=$2 i
    for i in {1..240}; do
        if jq -e --arg s "$want" '.state == $s' "$scratch/.config/ks/state/$name.json" >/dev/null 2>&1; then return 0; fi
        sleep 0.25
    done
    log "$name never reached $want"; return 1
}

# type_text sends text one character at a time so the recording shows typing.
type_text() {
    local id=$1 text=$2 i
    for ((i = 1; i <= ${#text}; i++)); do
        at send-text --match "id:$id" -- "${text[$i]}"
        sleep 0.03
    done
}

# capture grabs the OS window every ~200 ms until the stop file appears.
capture() {
    local pwid=$1 ms
    while [[ ! -e "$frames/stop" ]]; do
        ms=$(( ${EPOCHREALTIME%.*} * 1000 + ${${EPOCHREALTIME#*.}:0:3} ))
        screencapture -x -o -l "$pwid" "$frames/$ms.png"
        sleep 0.2
    done
}

# assemble turns the frames into the WebP, each shown for the time until the next.
assemble() {
    local -a files; local list="$frames/list.txt" i t next
    files=("$frames"/*.png(n))
    : > "$list"
    for ((i = 1; i <= ${#files}; i++)); do
        t=${${files[$i]:t}%.png}
        if (( i < ${#files} )); then next=${${files[$((i + 1))]:t}%.png}; else next=$(( t + 1500 )); fi
        print "file '${files[$i]}'" >> "$list"
        print "duration $(( (next - t) / 1000.0 ))" >> "$list"
    done
    ffmpeg -loglevel error -y -f concat -safe 0 -i "$list" \
        -vf 'scale=1200:-1:flags=lanczos' -c:v libwebp_anim -lossless 1 -loop 0 "$out"
    log "wrote $out (${#files} frames in $frames)"
}

main() {
    setup_home
    export HOME="$scratch"
    "$ks" new -n brain -d "$src/brain" >/dev/null
    "$ks" new -n scratch --agent shell -d "$src/scratch" >/dev/null
    "$ks" new -n kitty-session -d "$src/kitty-session" >/dev/null
    local bg=$(window brain) main=$(window kitty-session) side=$(sidebar kitty-session)
    local pwid=$(at ls | jq -r '.[0].platform_window_id')
    await_prompt "$bg"; await_screen "$main" 'trust this folder'
    # The background claude gets its question first; the dialog opens while
    # the camera is on the other tab, so its row turns red in the sidebar.
    type_text "$bg" "$prompt_bg"; at send-text --match "id:$bg" -- $'\r'
    sleep 4
    if jq -e '.state == "error"' "$scratch/.config/ks/state/brain.json" >/dev/null 2>&1; then
        log "brain failed its turn:"; at get-text --match "id:$bg" | grep -v '^\s*$' | tail -8 >&2
        "$ks" quit >/dev/null; exit 1
    fi
    at focus-window --match "id:$main"
    sleep 2
    capture "$pwid" &
    local cam=$!
    # The trust dialog is on screen and the row is red; down and enter accept it.
    sleep 3
    at send-text --match "id:$main" -- $'\x1b[B'
    sleep 0.5
    at send-text --match "id:$main" -- $'\r'
    await_prompt "$main"
    sleep 1.5
    type_text "$main" "$prompt_main"
    sleep 0.6
    at send-text --match "id:$main" -- $'\r'
    await_state kitty-session working || true
    await_state kitty-session idle || true
    sleep 3
    # Over to the sidebar, and 1 jumps to the first tab: the waiting question.
    at focus-window --match "id:$side"
    sleep 1.5
    at send-text --match "id:$side" -- 1
    sleep 4
    touch "$frames/stop"; wait $cam
    # The window may already be closed by hand; the frames are what matter.
    "$ks" quit >/dev/null 2>&1 || log "instance already gone"
    HOME="$real_home"
    assemble
}

# `split.sh assemble <frames dir>` rebuilds the WebP from an earlier run.
if [[ ${1:-} == assemble ]]; then
    frames=$2
    assemble
else
    main "$@"
fi
