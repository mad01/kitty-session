package claude

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Glyphs Claude Code prefixes to its tab title. Braille patterns (U+2800 to
// U+28FF) and the glyphs in workingGlyphs spin while it works; idleGlyph is
// shown at the prompt.
const (
	workingGlyphs = "◐◓◑◒·✢✶✻✽"
	idleGlyph     = '✳'
	brailleFirst  = 0x2800
	brailleLast   = 0x28FF
)

// ParseTitle reads the state glyph Claude Code puts in front of its tab title.
// It returns StateWorking for a spinner glyph and StateIdle for the idle
// glyph, with stripped holding the title minus the glyph and the spaces after
// it. A title without a known leading glyph returns ok false and is passed
// through unchanged.
//
// ✳ means idle: that is herdr's production rule and what Claude Code 2.1.289
// shows at the prompt. The title lags the hooks when a turn starts, so a
// backend combining both sources must let a fresh "working" state file
// override a ✳ title rather than the other way round.
func ParseTitle(title string) (state State, stripped string, ok bool) {
	r, size := utf8.DecodeRuneInString(title)
	switch {
	case size == 0 || r == utf8.RuneError:
		return StateUnknown, title, false
	case r == idleGlyph:
		state = StateIdle
	case isBraille(r) || strings.ContainsRune(workingGlyphs, r):
		state = StateWorking
	default:
		return StateUnknown, title, false
	}
	return state, strings.TrimLeftFunc(title[size:], unicode.IsSpace), true
}

func isBraille(r rune) bool {
	return r >= brailleFirst && r <= brailleLast
}
