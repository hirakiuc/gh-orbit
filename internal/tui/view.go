package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/glamour"
)

func (m *Model) View() tea.View {
	var viewContent string

	if m.showDetail {
		viewContent = m.renderDetailView()
	} else {
		// Build the view content from components
		viewContent = m.renderTabs()
		viewContent += "\n" + m.renderList()
	}

	footer := m.renderFooter()
	if footer != "" {
		viewContent += "\n" + footer
	}

	v := tea.NewView(viewContent)

	// Declarative terminal state
	v.AltScreen = true

	return v
}

func (m *Model) renderDetailView() string {
	if m.fetchingDetail {
		return m.spinner.View() + " Fetching detail..."
	}

	i, ok := m.list.SelectedItem().(item)
	if !ok {
		return "No item selected"
	}

	// Header
	header := m.styles.DetailHeader.Render(fmt.Sprintf("%s #%s", i.notification.SubjectTitle, extractNumberFromURL(i.notification.SubjectURL)))
	meta := fmt.Sprintf("Author: %s | Repo: %s | Reason: %s", i.notification.AuthorLogin, i.notification.RepositoryFullName, i.notification.Reason)
	
	content := header + "\n" + meta + "\n\n" + m.viewport.View()

	return m.styles.Viewport.Render(content)
}

func (m *Model) renderMarkdown(body string) string {
	if body == "" {
		return "No content available."
	}

	renderer, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(m.viewport.Width()-4),
	)
	if err != nil {
		return body
	}

	out, err := renderer.Render(body)
	if err != nil {
		return body
	}

	return out
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
