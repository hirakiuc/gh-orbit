package tui

import (
	"charm.land/lipgloss/v2"
)

// Styles defines the UI styles for the TUI.
type Styles struct {
	Title               lipgloss.Style
	Help                lipgloss.Style
	StatusNormal        lipgloss.Style
	StatusError         lipgloss.Style
	PriorityHigh        lipgloss.Style
	PriorityMed         lipgloss.Style
	PriorityLow         lipgloss.Style
	Cursor              lipgloss.Style
	SelectedTitle       lipgloss.Style
	SelectedDescription lipgloss.Style

	// Semantic colors for indicators
	Mention         lipgloss.Style
	ReviewRequested lipgloss.Style
	ActionRequired  lipgloss.Style
	Assign          lipgloss.Style
	Member          lipgloss.Style
	Subscribed      lipgloss.Style
	Unread          lipgloss.Style
	IconContainer   lipgloss.Style
}

// DefaultStyles returns the default styles for the application.
func DefaultStyles(isDark bool) Styles {
	s := Styles{}

	// Primary accent color
	accent := lipgloss.Color("#7D56F4")
	if !isDark {
		accent = lipgloss.Color("#5A3BD3") // Slightly darker for light backgrounds
	}

	fg := lipgloss.Color("#FAFAFA")
	if !isDark {
		fg = lipgloss.Color("#1A1A1A")
	}

	s.Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FAFAFA")).
		Background(accent).
		Padding(0, 1)

	s.Help = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#626262"))

	s.StatusNormal = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#00FF00"))

	s.StatusError = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FF0000"))

	s.PriorityHigh = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FF5F87"))

	s.PriorityMed = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFAF00"))

	s.PriorityLow = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#00AF87"))

	s.Cursor = lipgloss.NewStyle().
		Foreground(accent).
		Bold(true)

	s.SelectedTitle = lipgloss.NewStyle().
		Bold(true).
		Foreground(fg)

	s.SelectedDescription = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#A0A0A0"))

	// Semantic Indicators
	if isDark {
		s.Mention = lipgloss.NewStyle().Foreground(lipgloss.Color("#A371F7"))
		s.ReviewRequested = lipgloss.NewStyle().Foreground(lipgloss.Color("#D29922"))
		s.ActionRequired = lipgloss.NewStyle().Foreground(lipgloss.Color("#F85149"))
		s.Assign = lipgloss.NewStyle().Foreground(lipgloss.Color("#3FB950"))
		s.Member = lipgloss.NewStyle().Foreground(lipgloss.Color("#2F81F7"))
		s.Subscribed = lipgloss.NewStyle().Foreground(lipgloss.Color("#8B949E"))
		s.Unread = lipgloss.NewStyle().Foreground(lipgloss.Color("#58A6FF"))
	} else {
		s.Mention = lipgloss.NewStyle().Foreground(lipgloss.Color("#8957E5"))
		s.ReviewRequested = lipgloss.NewStyle().Foreground(lipgloss.Color("#9E6A03"))
		s.ActionRequired = lipgloss.NewStyle().Foreground(lipgloss.Color("#CF222E"))
		s.Assign = lipgloss.NewStyle().Foreground(lipgloss.Color("#1A7F37"))
		s.Member = lipgloss.NewStyle().Foreground(lipgloss.Color("#0969DA"))
		s.Subscribed = lipgloss.NewStyle().Foreground(lipgloss.Color("#6E7781"))
		s.Unread = lipgloss.NewStyle().Foreground(lipgloss.Color("#0969DA"))
	}

	s.IconContainer = lipgloss.NewStyle().Width(2)

	return s
}
