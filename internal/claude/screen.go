package claude

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	// screenTail is how many non-empty lines ParseScreen reads from the
	// bottom of the text: enough for a permission dialog with its options
	// and footer above the prompt box, few enough that an older dialog in
	// the scrollback cannot speak for the current screen.
	screenTail = 24
	// workingTail is how many of those lines the working signals are read
	// from: the status row and the spinner sit right above the prompt box,
	// and a spinner row further up is one Ink left behind.
	workingTail = 12
)

// Screen is what the bottom of a Claude Code window shows. The three
// signals are read from the text alone, so a window the user scrolled up
// in must be read with the live bottom included (kitty's get-text with
// --extent all) for any of them to mean anything.
type Screen struct {
	// Blocker is true while a dialog waits on the user: a permission prompt,
	// a question, a plan review, an MCP elicitation or the folder-trust
	// dialog.
	Blocker bool
	// PromptBox is true while the prompt box is at rest, its ❯ line between
	// the two horizontal rules, with no dialog on screen.
	PromptBox bool
	// Working is true while a turn runs: the status row says esc to
	// interrupt, a spinner is on screen, or Claude waits on its background
	// agents.
	Working bool
}

// The rule set follows the claude manifest of herdr (src/detect/manifests/
// claude.toml, Apache 2.0): the footer strings, the option-line shapes and
// the regions those are looked for in. Only the rules a sidebar needs are
// kept, written as Go here.
const (
	// ruleRunes are the box-drawing characters a horizontal rule is made of.
	ruleRunes = "─━"
	// ruleShare is the least share of a trimmed line's runes that must be
	// rule characters for the line to count as a horizontal rule.
	ruleShare = 0.8
	// ruleMinRunes is the least number of rule characters in a rule.
	ruleMinRunes = 3
	// overlayLines is how many of the last lines name the transcript viewer.
	overlayLines = 3
	// activityGlyphs lead an activity line: the spinner frames of the title
	// (workingGlyphs), the ✳ frame that doubles as the idle glyph, and the
	// * Claude Code draws where the glyphs are not available.
	activityGlyphs = workingGlyphs + string(idleGlyph) + "*"
)

var (
	// navigateHints are the footer hints a menu shows beside "enter to
	// select"; one of them makes that footer a blocker.
	navigateHints = []string{
		"tab/arrow keys to navigate",
		"arrow keys to navigate",
		"arrows to navigate",
		"↑/↓ to navigate",
		"↑↓ to navigate",
	}
	// dialogHints are the footer fragments that never appear in a prompt box
	// at rest; one of them inside the box body means a dialog took its place.
	dialogHints = []string{
		"enter to select",
		"esc to cancel",
		"tab/arrow keys",
		"arrow keys to navigate",
		"↑/↓ to navigate",
	}
	// blockerPhrases each mark a dialog on their own, wherever in the tail
	// they appear.
	blockerPhrases = []string{
		"review your answers",
		"skip interview and plan immediately",
		"waiting for permission",
		"do you want to allow this connection?",
	}
	// promptLine is the ❯ line of the prompt box or of a selected option.
	promptLine = regexp.MustCompile(`^\s*❯`)
	// yesOption and numberedOption are the option lines of a permission
	// dialog, with or without the ❯ cursor on them.
	yesOption      = regexp.MustCompile(`(?i)^\s*❯?\s*(?:1\.\s*)?yes\b`)
	numberedOption = regexp.MustCompile(`(?i)^\s*❯?\s*[23]\.\s*(?:yes|no)\b`)
	// trustOption is the first option of the folder-trust dialog.
	trustOption = regexp.MustCompile(`(?i)^\s*❯?\s*1\.\s*yes`)
	// elicitHeader and elicitOption are the header and the controls of an
	// MCP elicitation dialog, which has no Enter hint in its footer.
	elicitHeader = regexp.MustCompile(`(?i)^\s*MCP server ["“].+["”] requests your input\s*$`)
	elicitOption = regexp.MustCompile(`^\s*❯?\s*(?:Accept|Decline)\b`)
	// interruptLine is the status row of a running turn.
	interruptLine = regexp.MustCompile(`^\s*[⏸⏵].*esc to interrupt`)
	// activityLine is an activity glyph, an activity word and an ellipsis,
	// with or without the elapsed time after it.
	activityLine = regexp.MustCompile(
		`^\s*[` + regexp.QuoteMeta(activityGlyphs) + `]\s+\S.*…(?:\s+\(\d+[smh]|\s*$)`,
	)
)

// LiveScreen cuts text, as GetTextAll returns it, to its last rows lines,
// empty lines included: the live screen of a window rows lines high. A
// trailing newline ends the last line rather than starting an empty one.
// rows of zero or less returns text unchanged.
func LiveScreen(text string, rows int) string {
	if rows <= 0 {
		return text
	}
	body, nl := strings.CutSuffix(text, "\n")
	lines := strings.Split(body, "\n")
	if len(lines) > rows {
		lines = lines[len(lines)-rows:]
	}
	out := strings.Join(lines, "\n")
	if nl {
		out += "\n"
	}
	return out
}

// ParseScreen reads the last screenTail non-empty lines of a window's text
// and reports what Claude Code shows there. An overlay that hides the state
// (the transcript viewer, the model picker) reports nothing, so a caller
// leaves its last state alone. PromptBox is never true beside Blocker: a
// dialog's own ❯ cursor is not the prompt.
func ParseScreen(text string) Screen {
	tail := tailLines(text, screenTail)
	lower := lowerLines(tail)
	if len(tail) == 0 || isOverlay(lower) {
		return Screen{}
	}
	body, after := ruleRegions(tail, lower)
	s := Screen{
		Blocker: isBlocker(tail, lower, after),
		Working: isWorking(tail[max(len(tail)-workingTail, 0):]),
	}
	s.PromptBox = !s.Blocker && isPromptBox(body)
	return s
}

// tailLines returns the last n non-empty lines of text in screen order.
func tailLines(text string, n int) []string {
	lines := strings.Split(text, "\n")
	var tail []string
	for i := len(lines) - 1; i >= 0 && len(tail) < n; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			tail = append(tail, lines[i])
		}
	}
	for i, j := 0, len(tail)-1; i < j; i, j = i+1, j-1 {
		tail[i], tail[j] = tail[j], tail[i]
	}
	return tail
}

// lowerLines lowercases every line for the contains checks.
func lowerLines(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.ToLower(l)
	}
	return out
}

// region is one stretch of the tail with its lowercase twin.
type region struct {
	lines []string
	lower []string
}

// contains reports whether the region's text holds phrase, case-folded.
func (r region) contains(phrase string) bool {
	for _, l := range r.lower {
		if strings.Contains(l, phrase) {
			return true
		}
	}
	return false
}

// containsAny reports whether the region holds any of the phrases.
func (r region) containsAny(phrases []string) bool {
	for _, p := range phrases {
		if r.contains(p) {
			return true
		}
	}
	return false
}

// hasLine reports whether any line of the region matches re.
func (r region) hasLine(re *regexp.Regexp) bool {
	for _, l := range r.lines {
		if re.MatchString(l) {
			return true
		}
	}
	return false
}

// ruleRegions splits the tail at its horizontal rules: body is the prompt
// box body, the lines between the second-to-last rule and the last one,
// and after is everything below the last rule, or the whole tail when
// there is none.
func ruleRegions(tail, lower []string) (body, after region) {
	last, prev := -1, -1
	for i, l := range tail {
		if isRule(l) {
			prev, last = last, i
		}
	}
	if last < 0 {
		return region{}, region{lines: tail, lower: lower}
	}
	after = region{lines: tail[last+1:], lower: lower[last+1:]}
	if prev >= 0 {
		body = region{lines: tail[prev+1 : last], lower: lower[prev+1 : last]}
	}
	return body, after
}

// isRule reports whether line is a horizontal rule: trimmed, at least
// ruleMinRunes rule characters making up at least ruleShare of its runes.
func isRule(line string) bool {
	line = strings.TrimSpace(line)
	total := utf8.RuneCountInString(line)
	rules := 0
	for _, r := range line {
		if strings.ContainsRune(ruleRunes, r) {
			rules++
		}
	}
	return rules >= ruleMinRunes && float64(rules) >= ruleShare*float64(total)
}

// isOverlay reports whether a screen that hides the state is up: the
// transcript viewer, named in its last lines, or the model picker. lower
// is the lowercased tail.
func isOverlay(lower []string) bool {
	last := region{lower: lower[max(len(lower)-overlayLines, 0):]}
	if last.contains("showing detailed transcript") {
		return true
	}
	all := region{lower: lower}
	return all.contains("select model") && all.contains("enter to set as default")
}

// isBlocker reports whether a dialog waits on the user, by the footer below
// the last rule, the option lines of a permission dialog, the controls of
// an MCP elicitation, one of the phrases that only a dialog shows, or the
// folder-trust dialog.
func isBlocker(tail, lower []string, after region) bool {
	all := region{lines: tail, lower: lower}
	switch {
	case after.contains("esc to cancel") && (after.contains("enter to confirm") ||
		(after.contains("enter to select") && after.containsAny(navigateHints))):
		return true
	case all.contains("do you want to proceed?") &&
		(all.hasLine(yesOption) || all.hasLine(numberedOption)):
		return true
	case all.hasLine(elicitHeader) && all.contains("esc to cancel") &&
		all.hasLine(elicitOption):
		return true
	case all.containsAny(blockerPhrases):
		return true
	case all.contains("run a dynamic workflow?") && all.contains("esc to cancel"):
		return true
	case all.contains("trust the files") &&
		(all.contains("enter to confirm") || all.hasLine(trustOption)):
		return true
	}
	return false
}

// isPromptBox reports whether body is the prompt box at rest: a ❯ line and
// none of a dialog's footer hints.
func isPromptBox(body region) bool {
	return body.hasLine(promptLine) && !body.containsAny(dialogHints)
}

// isWorking reports whether a turn is running in the lines it is given: the
// esc-to-interrupt status row, an activity line with its ellipsis, the wait
// on background agents, or a braille spinner frame at the start of a line.
func isWorking(lines []string) bool {
	for _, l := range lines {
		if interruptLine.MatchString(l) || activityLine.MatchString(l) {
			return true
		}
		if i := strings.Index(l, "Waiting for"); i >= 0 &&
			strings.Contains(l[i:], "background agents to finish") {
			return true
		}
		if r, _ := utf8.DecodeRuneInString(strings.TrimLeft(l, " \t")); isBraille(r) {
			return true
		}
	}
	return false
}
