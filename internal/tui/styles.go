package tui

import "github.com/charmbracelet/lipgloss"

// Color palette
const (
	colorAccent    = lipgloss.Color("#7571F9") // indigo
	colorSuccess   = lipgloss.Color("#02BF87") // teal/green
	colorAmber     = lipgloss.Color("#FFBF00") // amber
	colorMuted     = lipgloss.Color("#636363") // gray
	colorDanger    = lipgloss.Color("#ED567A") // coral
	colorTextPri   = lipgloss.Color("#FFFDF5") // cream
	colorTextSec   = lipgloss.Color("#C1C6B2") // light gray
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
	stoppedBadge = lipgloss.NewStyle().Foreground(colorMuted).SetString("○ stopped")

	// State badges — selected
	selectedIdleBadge    = lipgloss.NewStyle().Foreground(colorSuccess).Bold(true).SetString("● idle")
	selectedStoppedBadge = lipgloss.NewStyle().Foreground(colorTextSec).SetString("○ stopped")

	// Selected row
	cursorStyle       = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	selectedNameStyle = lipgloss.NewStyle().Foreground(colorTextPri).Bold(true)
	selectedDirStyle  = lipgloss.NewStyle().Foreground(colorTextSec)

	// Normal row
	normalStyle = lipgloss.NewStyle().Foreground(colorTextSec)
	dirStyle    = lipgloss.NewStyle().Foreground(colorMuted)

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
