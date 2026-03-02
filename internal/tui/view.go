package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

func (m *Model) View() tea.View {
	// Root view
	viewContent := m.list.View()

	// Status and Error handling display
	var footer string
	if m.syncing {
		footer = m.spinner.View() + " Syncing... "
	}

	// Priority: Error > Status
	if m.err != nil {
		footer += m.styles.StatusError.Render(fmt.Sprintf(" Error: %v ", m.err))
	} else if m.status != "" {
		footer += m.styles.StatusNormal.Render(fmt.Sprintf(" %s ", m.status))
	}

	if footer != "" {
		// Ensure the footer is exactly one line and truncated if necessary
		// We use Height-1 in Update for the list, so we must stay within 1 line.
		viewContent += "\n" + footer
	}

	v := tea.NewView(viewContent)

	// Declarative terminal state
	v.AltScreen = true

	return v
}
