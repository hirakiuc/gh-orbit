package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/glamour"
)

func (m *Model) View() tea.View {
	var viewContent string
	windowTitle := "gh-orbit"

	switch m.state {
	case StateDetail:
		if i, ok := m.list.SelectedItem().(item); ok {
			windowTitle = fmt.Sprintf("gh-orbit: %s", i.notification.SubjectTitle)
		}
		viewContent = m.renderDetailView()
	case StateList:
		// Build the view content from components
		viewContent = m.renderTabs()
		viewContent += "\n" + m.renderList()
	}

	footer := m.renderFooter()
	if footer != "" {
		viewContent += "\n" + footer
	}

	// Delegate overlays to UIController
	content := m.ui.View(
		viewContent,
		m.state == StateDetail,
		m.viewport.ScrollPercent(),
		m.viewport.Height(),
		m.viewport.TotalLineCount(),
	)

	v := tea.NewView(content)

	// Declarative terminal state
	v.AltScreen = true
	v.WindowTitle = windowTitle
	v.MouseMode = tea.MouseModeCellMotion

	return v
}

func (m *Model) renderDetailView() string {
	i, ok := m.list.SelectedItem().(item)
	if !ok {
		return "No item selected"
	}

	// 1. Unified Header using shared component
	headerCtx := RenderContext{
		Styles:     m.styles,
		Width:      m.width,
		IsFetching: m.ui.fetchingDetail,
	}
	header := RenderTargetHeader(headerCtx, i.notification, "", true)
	
	meta := fmt.Sprintf("Author: %s | Repo: %s", i.notification.AuthorLogin, i.notification.RepositoryFullName)
	
	// Content inside the viewport or loading indicator
	body := m.viewport.View()
	if m.ui.fetchingDetail {
		body = "\n\n  " + m.ui.RenderSpinner() + " Fetching detail..."
	}
	
	content := header + "\n" + meta + "\n\n" + body

	// If we have dimensions, ensure the style doesn't clip
	style := m.styles.Viewport
	if m.width > 0 && m.height > 0 {
		style = style.Width(m.width - 2).Height(m.height - 4)
	}

	return style.Render(content)
}

func (m *Model) renderMarkdown(body string) string {
	if body == "" {
		return "No content available."
	}

	if m.markdownRenderer == nil {
		m.updateMarkdownRenderer()
	}

	out, err := m.markdownRenderer.Render(body)
	if err != nil {
		return body
	}

	return out
}

func (m *Model) updateMarkdownRenderer() {
	width := m.viewport.Width() - 4
	if width < 20 {
		width = 20
	}

	style := "dark"
	if !m.isDark {
		style = "light"
	}

	renderer, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(style),
		glamour.WithEmoji(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		m.logger.Error("failed to create markdown renderer", "error", err)
		return
	}
	m.markdownRenderer = renderer
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
	spinner := m.ui.RenderSpinner()
	if spinner != "" {
		footer = spinner + " Syncing... "
	}

	// Priority: Error only (status is now toasts)
	if m.err != nil {
		footer += m.styles.StatusError.Render(fmt.Sprintf(" Error: %v ", m.err))
	}

	return footer
}
