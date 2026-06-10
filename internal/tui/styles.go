package tui

import "github.com/charmbracelet/lipgloss"

var (
	colorBorder    = lipgloss.Color("238")
	colorSubtle    = lipgloss.Color("240")
	colorPrimary   = lipgloss.Color("75")
	colorAccent    = lipgloss.Color("213")
	colorWarn      = lipgloss.Color("214")
	colorError     = lipgloss.Color("196")
	colorSuccess   = lipgloss.Color("82")
	colorForresten = lipgloss.Color("135")

	styleUser = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	styleUserBody = lipgloss.NewStyle().
			BorderLeft(true).
			BorderStyle(lipgloss.ThickBorder()).
			BorderForeground(colorPrimary).
			PaddingLeft(1)

	styleAssistantLabel = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	styleAssistantBody = lipgloss.NewStyle().
				Foreground(lipgloss.Color("252"))

	styleAssistant = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	styleForrestenLabel = lipgloss.NewStyle().
				Foreground(colorForresten).
				Bold(true)

	styleForrestenBody = lipgloss.NewStyle().
				BorderLeft(true).
				BorderStyle(lipgloss.ThickBorder()).
				BorderForeground(colorForresten).
				PaddingLeft(1)

	styleSystem = lipgloss.NewStyle().
			Foreground(colorSubtle).
			Italic(true)

	styleBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder)

	styleActiveBorder = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorPrimary)

	styleStatusBar = lipgloss.NewStyle().
			Background(lipgloss.Color("235")).
			Foreground(colorSubtle).
			Padding(0, 1)

	styleContextOk   = lipgloss.NewStyle().Foreground(colorSuccess)
	styleContextWarn = lipgloss.NewStyle().Foreground(colorWarn)
	styleContextCrit = lipgloss.NewStyle().Foreground(colorError)

	styleSlashCmd = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	styleSuccess = lipgloss.NewStyle().Foreground(colorSuccess)
	styleError   = lipgloss.NewStyle().Foreground(colorError)
)
