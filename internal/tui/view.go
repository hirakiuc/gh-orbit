package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

func (m Model) View() tea.View {
	// Root view
	viewContent := m.list.View()

	// Error handling display
	if m.err != nil {
		errorView := m.styles.StatusError.Render(fmt.Sprintf(" Error: %v ", m.err))
		viewContent += "\n" + errorView
	}

	v := tea.NewView(viewContent)

	// Declarative terminal state
	v.AltScreen = true

	return v
}
