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

	// Tabs
	TabActive    lipgloss.Style
	TabInactive  lipgloss.Style
	TabContainer lipgloss.Style

	// Detail View
	DetailHeader lipgloss.Style
	DetailBody   lipgloss.Style
	DetailBadge  lipgloss.Style
	Viewport     lipgloss.Style

	// Overlays
	Toast          lipgloss.Style
	ScrollbarThumb lipgloss.Style
	FilterChip     lipgloss.Style

	// Resource States
	StateOpen   lipgloss.Style
	StateMerged lipgloss.Style
	StateClosed lipgloss.Style
	StateDraft  lipgloss.Style

	// Search
	FuzzyMatch lipgloss.Style
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
		s.Unread = lipgloss.NewStyle().Foreground(lipgloss.Color("#2F81F7")) // Brighter Blue
	} else {
		s.Mention = lipgloss.NewStyle().Foreground(lipgloss.Color("#8957E5"))
		s.ReviewRequested = lipgloss.NewStyle().Foreground(lipgloss.Color("#9E6A03"))
		s.ActionRequired = lipgloss.NewStyle().Foreground(lipgloss.Color("#D1242F"))
		s.Assign = lipgloss.NewStyle().Foreground(lipgloss.Color("#1A7F37"))
		s.Member = lipgloss.NewStyle().Foreground(lipgloss.Color("#0969DA"))
		s.Subscribed = lipgloss.NewStyle().Foreground(lipgloss.Color("#6E7781"))
		s.Unread = lipgloss.NewStyle().Foreground(lipgloss.Color("#0969DA"))
	}

	s.IconContainer = lipgloss.NewStyle().Width(2)

	// Tabs
	s.TabActive = lipgloss.NewStyle().
		Bold(true).
		Foreground(accent).
		Padding(0, 1).
		Underline(true)

	s.TabInactive = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#626262")).
		Padding(0, 1)

	s.TabContainer = lipgloss.NewStyle().
		Height(1).
		MarginBottom(1)

	s.DetailHeader = lipgloss.NewStyle().
		Bold(true).
		Foreground(accent).
		Padding(0, 1).
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(lipgloss.Color("#30363D"))

	s.DetailBody = lipgloss.NewStyle().
		Padding(1, 2)

	s.DetailBadge = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(accent).
		Padding(0, 1).
		Bold(true)

	s.Viewport = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#30363D"))

	s.Toast = lipgloss.NewStyle().
		Padding(0, 1).
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(accent).
		Bold(true)

	s.ScrollbarThumb = lipgloss.NewStyle().
		Foreground(accent).
		Background(accent)

	s.FilterChip = lipgloss.NewStyle().
		Padding(0, 1).
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#DB6109")).
		Bold(true)

	s.FuzzyMatch = lipgloss.NewStyle().
		Foreground(accent).
		Bold(true).
		Underline(true)

	// Resource States (Desaturated Professional Palette)
	openColor := lipgloss.Color("#2EA043")   // Dark
	mergedColor := lipgloss.Color("#8957E5") // Dark
	closedColor := lipgloss.Color("#F85149") // Dark
	draftColor := lipgloss.Color("#8B949E")  // Dark

	if !isDark {
		openColor = lipgloss.Color("#1A7F37")
		mergedColor = lipgloss.Color("#6E39D1")
		closedColor = lipgloss.Color("#D1242F")
		draftColor = lipgloss.Color("#6E7781")
	}

	s.StateOpen = lipgloss.NewStyle().Padding(0, 1).Foreground(openColor)
	s.StateMerged = lipgloss.NewStyle().Padding(0, 1).Foreground(mergedColor)
	s.StateClosed = lipgloss.NewStyle().Padding(0, 1).Foreground(closedColor)
	s.StateDraft = lipgloss.NewStyle().Padding(0, 1).Foreground(draftColor)

	return s
}
