package tui

import "github.com/charmbracelet/lipgloss"

// Color palette — AdaptiveColor{Light, Dark}
var (
	colorAccent  = lipgloss.AdaptiveColor{Light: "#A626A4", Dark: "#7571F9"} // magenta / indigo
	colorSuccess = lipgloss.AdaptiveColor{Light: "#40A14F", Dark: "#02BF87"} // green
	colorAmber   = lipgloss.AdaptiveColor{Light: "#C18401", Dark: "#FFBF00"} // yellow/amber
	colorMuted   = lipgloss.AdaptiveColor{Light: "#A0A1A7", Dark: "#636363"} // gray
	colorDanger  = lipgloss.AdaptiveColor{Light: "#E45649", Dark: "#ED567A"} // red / coral
	colorTextPri = lipgloss.AdaptiveColor{Light: "#383A42", Dark: "#FFFDF5"} // foreground
	colorTextSec = lipgloss.AdaptiveColor{Light: "#696C77", Dark: "#C1C6B2"} // dimmed foreground
)

var (
	// Frame around the whole TUI
	frameStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorAccent).
			Padding(1, 2)

	// Title bar (used as list title style)
	titleBarStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorTextPri).
			Background(colorAccent).
			Padding(0, 1)

	// State badges — static (working and input badges are rendered dynamically with pulse animation)
	idleBadge    = lipgloss.NewStyle().Foreground(colorSuccess).SetString("● idle")
	waitingBadge = lipgloss.NewStyle().Foreground(colorTextSec).SetString("○ waiting")
	stoppedBadge = lipgloss.NewStyle().Foreground(colorMuted).SetString("○ stopped")

	// State badges — selected
	selectedIdleBadge    = lipgloss.NewStyle().Foreground(colorSuccess).Bold(true).SetString("● idle")
	selectedWaitingBadge = lipgloss.NewStyle().Foreground(colorTextSec).Bold(true).SetString("○ waiting")
	selectedStoppedBadge = lipgloss.NewStyle().Foreground(colorTextSec).SetString("○ stopped")

	// Selected row
	cursorStyle       = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	selectedNameStyle = lipgloss.NewStyle().Foreground(colorTextPri).Bold(true)
	selectedDirStyle  = lipgloss.NewStyle().Foreground(colorTextSec)

	// Normal row
	normalStyle = lipgloss.NewStyle().Foreground(colorTextSec)
	dirStyle    = lipgloss.NewStyle().Foreground(colorMuted)

	// Tmp repo item (amber)
	tmpSelectedStyle = lipgloss.NewStyle().Foreground(colorAmber).Bold(true)
	tmpNormalStyle   = lipgloss.NewStyle().Foreground(colorAmber)

	// Input prompt
	inputPromptStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	inputStyle       = lipgloss.NewStyle().Foreground(colorTextPri)

	// Confirm & error
	confirmStyle = lipgloss.NewStyle().Foreground(colorDanger).Bold(true)
	errorStyle   = lipgloss.NewStyle().Foreground(colorDanger)

	// Help (inline bar)
	helpBarStyle        = lipgloss.NewStyle().Foreground(colorMuted)
	helpKeyInlineStyle  = lipgloss.NewStyle().Foreground(colorAccent)

	// Full help screen
	helpTitleStyle   = lipgloss.NewStyle().Bold(true).Foreground(colorAccent).MarginBottom(1)
	helpSectionStyle = lipgloss.NewStyle().Bold(true).Foreground(colorSuccess).MarginTop(1)
	helpKeyStyle     = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Width(14)
	helpDescStyle    = lipgloss.NewStyle().Foreground(colorTextSec)

	// Context preview (second line under each session)
	contextStyle = lipgloss.NewStyle().Foreground(colorMuted)

	// Quit confirmation dialog
	quitDialogStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorAccent).
			Padding(1, 3).
			Align(lipgloss.Center)

	// List component styles
	statusBarStyle = lipgloss.NewStyle().
			Foreground(colorTextSec).
			Padding(0, 0, 1, 2)

	filterPromptStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	filterCursorStyle = lipgloss.NewStyle().Foreground(colorAccent)
	noItemsStyle      = lipgloss.NewStyle().Foreground(colorMuted).Padding(0, 2)
	helpStyle         = lipgloss.NewStyle().Foreground(colorMuted).Padding(1, 0, 0, 2)
	paginationStyle   = lipgloss.NewStyle().Foreground(colorMuted).PaddingLeft(2)
	activeDotStyle    = lipgloss.NewStyle().Foreground(colorAccent)
	inactiveDotStyle  = lipgloss.NewStyle().Foreground(colorMuted)

)
