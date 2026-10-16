# Sidebar demo for the README, recorded with vhs (https://github.com/charmbracelet/vhs).
#
# `make demo` runs it from the repo root with the branch build of ks on PATH.
# It renders images/sidebar.gif and the still frames the README embeds, all
# from `ks _sidebar-demo`, so no kitty instance or Claude session is involved.
# The look is the author's kitty setup: PragmataPro Mono and the colours of
# the Catppuccin Macchiato theme the kitty config includes. A machine
# without that font falls back to whatever the browser picks.
#
# A Screenshot saves the next frame the recorder captures, so a Sleep
# follows each one; without it the next key lands before that frame.

Output images/sidebar.gif

Set Shell bash
Set FontFamily "PragmataPro Mono"
Set FontSize 20
Set Width 440
Set Height 656
Set Padding 16
Set WindowBar Colorful
Set Framerate 24
Set TypingSpeed 60ms
Set Theme { "name": "kitty catppuccin macchiato", "background": "#24273a", "foreground": "#cad3f5", "selection": "#48d597", "cursor": "#f4dbd6", "black": "#494d64", "red": "#ed8796", "green": "#a6da95", "yellow": "#eed49f", "blue": "#8aadf4", "magenta": "#f5bde6", "cyan": "#8bd5ca", "white": "#b8c0e0", "brightBlack": "#5b6078", "brightRed": "#ed8796", "brightGreen": "#a6da95", "brightYellow": "#eed49f", "brightBlue": "#8aadf4", "brightMagenta": "#f5bde6", "brightCyan": "#8bd5ca", "brightWhite": "#a5adcb" }

Hide
Type "ks _sidebar-demo"
Enter
Sleep 1.5s
Show

# The agent list: a dot per state, the name, Claude's title or the directory.
Sleep 3s
Screenshot images/sidebar.png
Sleep 1s

Down
Sleep 500ms
Down
Sleep 500ms
Down
Sleep 1.5s

# The menu holds every action.
Type "m"
Sleep 2.5s
Screenshot images/sidebar-menu.png
Sleep 1s
Escape
Sleep 1s

# New agent: the repo picker, fuzzy filtered, then the kind.
Type "n"
Sleep 2s
Screenshot images/sidebar-picker.png
Sleep 1s
Type "ralph"
Sleep 1.5s
Enter
Sleep 2s
Screenshot images/sidebar-kind.png
Sleep 1s
Down
Sleep 800ms
Enter
Sleep 3s

# The keys popup.
Type "?"
Sleep 2.5s
Screenshot images/sidebar-keys.png
Sleep 1s
Escape
Sleep 2s
