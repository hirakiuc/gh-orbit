package tui

import "charm.land/lipgloss/v2"

// Styles defines the UI styles for the TUI.
type Styles struct {
	Title        lipgloss.Style
	Help         lipgloss.Style
	StatusNormal lipgloss.Style
	StatusError  lipgloss.Style
	PriorityHigh lipgloss.Style
	PriorityMed  lipgloss.Style
	PriorityLow  lipgloss.Style
}

// DefaultStyles returns the default styles for the application.
func DefaultStyles() Styles {
	s := Styles{}

	s.Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FAFAFA")).
		Background(lipgloss.Color("#7D56F4")).
		Padding(0, 1)

	s.Help = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#626262"))

	s.StatusNormal = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#00FF00"))

	s.StatusError = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FF0000"))

	s.PriorityHigh = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5F87"))
	s.PriorityMed = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFAF00"))
	s.PriorityLow = lipgloss.NewStyle().Foreground(lipgloss.Color("#00AF87"))

	return s
}
