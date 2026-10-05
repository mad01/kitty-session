package sidebar

import "github.com/charmbracelet/lipgloss"

// Palette as AdaptiveColor{Light, Dark}, shared with the rest of ks.
var (
	colorAccent  = lipgloss.AdaptiveColor{Light: "#A626A4", Dark: "#7571F9"} // magenta / indigo
	colorSuccess = lipgloss.AdaptiveColor{Light: "#40A14F", Dark: "#02BF87"} // green
	colorTeal    = lipgloss.AdaptiveColor{Light: "#0B8F8F", Dark: "#2DD4BF"} // teal
	colorMuted   = lipgloss.AdaptiveColor{Light: "#A0A1A7", Dark: "#636363"} // gray
	colorFocus   = lipgloss.AdaptiveColor{Light: "#7BC96F", Dark: "#A6E3A1"} // light green
	colorDanger  = lipgloss.AdaptiveColor{Light: "#E45649", Dark: "#ED567A"} // red / coral
	colorTextPri = lipgloss.AdaptiveColor{Light: "#383A42", Dark: "#FFFDF5"} // foreground
	colorTextSec = lipgloss.AdaptiveColor{Light: "#696C77", Dark: "#C1C6B2"} // dimmed foreground
	colorOwnBG   = lipgloss.AdaptiveColor{Light: "#ECECEE", Dark: "#262A33"} // this tab's row
	colorPickBG  = lipgloss.AdaptiveColor{Light: "#DDDDF5", Dark: "#363B57"} // cursor row
)

// Pulse cycles for the two states that animate: bright, medium, dim, medium.
var (
	workingPulse = []lipgloss.AdaptiveColor{
		{Light: "#C18401", Dark: "#FFBF00"},
		{Light: "#D9A441", Dark: "#CC9900"},
		{Light: "#E8C98A", Dark: "#997300"},
		{Light: "#D9A441", Dark: "#CC9900"},
	}
	inputPulse = []lipgloss.AdaptiveColor{
		{Light: "#E45649", Dark: "#ED567A"},
		{Light: "#EC8A81", Dark: "#C04562"},
		{Light: "#F4BDB8", Dark: "#8F3449"},
		{Light: "#EC8A81", Dark: "#C04562"},
	}
)

var (
	borderStyle = lipgloss.NewStyle().Foreground(colorMuted)

	headerStyle     = lipgloss.NewStyle().Foreground(colorTextPri).Bold(true)
	sortLabelStyle  = lipgloss.NewStyle().Foreground(colorTextSec)
	footerStyle     = lipgloss.NewStyle().Foreground(colorAccent)
	footerHintStyle = lipgloss.NewStyle().Foreground(colorMuted)

	nameStyle   = lipgloss.NewStyle().Foreground(colorTextPri).Bold(true)
	titleStyle  = lipgloss.NewStyle().Foreground(colorMuted)
	markerStyle = lipgloss.NewStyle().Foreground(colorAccent)
	plainStyle  = lipgloss.NewStyle()

	dotDone    = lipgloss.NewStyle().Foreground(colorTeal)
	dotIdle    = lipgloss.NewStyle().Foreground(colorSuccess)
	dotStopped = lipgloss.NewStyle().Foreground(colorMuted)

	filterPromptStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	inputStyle        = lipgloss.NewStyle().Foreground(colorTextPri)
	inputCursorStyle  = lipgloss.NewStyle().Foreground(colorAccent)

	statusStyle = lipgloss.NewStyle().Foreground(colorTextSec)
	errorStyle  = lipgloss.NewStyle().Foreground(colorDanger)

	popupItemStyle     = lipgloss.NewStyle().Foreground(colorTextPri)
	popupSelectedStyle = lipgloss.NewStyle().
				Foreground(colorTextPri).
				Bold(true).
				Background(colorPickBG)
	popupTitleStyle = lipgloss.NewStyle().Foreground(colorDanger).Bold(true)
	popupHintStyle  = lipgloss.NewStyle().Foreground(colorMuted)

	pickerNameStyle     = lipgloss.NewStyle().Foreground(colorTextSec)
	pickerSelectedStyle = lipgloss.NewStyle().
				Foreground(colorTextPri).
				Bold(true).
				Background(colorPickBG)
	pickerTmpStyle = lipgloss.NewStyle().Foreground(colorAccent)
)

// frameStyle styles the sidebar's outer frame: light green while the sidebar
// window has keyboard focus, muted otherwise, so the frame says which side of
// the tab the keys go to. The popups keep borderStyle whatever the focus.
func frameStyle(focused bool) lipgloss.Style {
	if focused {
		return lipgloss.NewStyle().Foreground(colorFocus)
	}
	return borderStyle
}

// Glyphs.
const (
	dotFilled = "●"
	dotHollow = "○"
	dotFaint  = "·"
	ownMarker = "▌"
	ellipsis  = "…"
)
