package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func (m *Model) View() tea.View {
	// Build the view content from components
	viewContent := m.renderTabs()
	viewContent += "\n" + m.renderList()

	footer := m.renderFooter()
	if footer != "" {
		viewContent += "\n" + footer
	}

	v := tea.NewView(viewContent)

	// Declarative terminal state
	v.AltScreen = true

	return v
}

func (m *Model) renderTabs() string {
	tabNames := []string{"Inbox", "Unread", "Triaged", "All"}
	var renderedTabs []string

	for i, name := range tabNames {
		var style lipgloss.Style
		if i == m.activeTab {
			style = m.styles.TabActive
		} else {
			style = m.styles.TabInactive
		}
		renderedTabs = append(renderedTabs, style.Render(name))
	}

	row := lipgloss.JoinHorizontal(lipgloss.Top, renderedTabs...)
	return m.styles.TabContainer.Render(row)
}

func (m *Model) renderList() string {
	return m.list.View()
}

func (m *Model) renderFooter() string {
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

	return footer
}
