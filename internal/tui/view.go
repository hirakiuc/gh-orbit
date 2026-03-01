package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

func (m Model) View() tea.View {
	// Root view
	v := tea.NewView(m.list.View())

	// Declarative terminal state
	v.AltScreen = true

	// Error handling display
	if m.err != nil {
		errorView := m.styles.StatusError.Render(fmt.Sprintf("Error: %v", m.err))
		// Prepend or append error view if needed, but list view takes full screen.
		// For MVP, we'll just log or show briefly if list allows.
		// Actually, let's just use the list's status bar for now if possible.
		_ = errorView // TODO: Display error
	}

	return v
}
