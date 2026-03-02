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

	return s
}
