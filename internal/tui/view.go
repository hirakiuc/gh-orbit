package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/dustin/go-humanize"
	"github.com/charmbracelet/glamour"
)

func (m *Model) View() tea.View {
	var viewContent string
	windowTitle := "gh-orbit"

	switch m.state {
	case StateDetail:
		viewContent = m.renderDetailView()
		if i, ok := m.listView.list.SelectedItem().(item); ok {
			windowTitle = fmt.Sprintf("%s - %s", windowTitle, i.notification.SubjectTitle)
		}
	case StateList:
		viewContent = m.renderList()
	}

	// Compose with overlays
	content := m.ui.View(
		viewContent,
		m.state == StateDetail,
		m.detailView.viewport.ScrollPercent(),
		m.detailView.viewport.Height(),
		m.detailView.viewport.TotalLineCount(),
	)

	v := tea.NewView(content)

	// Declarative terminal state
	v.AltScreen = true
	v.WindowTitle = windowTitle
	v.MouseMode = tea.MouseModeCellMotion

	return v
}

func (m *Model) renderDetailView() string {
	i, ok := m.listView.list.SelectedItem().(item)
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
	body := m.detailView.viewport.View()
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

func (m *Model) renderList() string {
	tabs := m.renderTabs()
	list := m.listView.list.View()
	return lipgloss.JoinVertical(lipgloss.Left, tabs, list, m.renderFooter())
}

func (m *Model) renderTabs() string {
	var tabs []string
	labels := []string{"Inbox", "Unread", "Triaged", "All"}

	for i, label := range labels {
		if i == m.listView.activeTab {
			tabs = append(tabs, m.styles.TabActive.Render(label))
		} else {
			tabs = append(tabs, m.styles.TabInactive.Render(label))
		}
	}

	return m.styles.TabContainer.Render(lipgloss.JoinHorizontal(lipgloss.Top, tabs...))
}

func (m *Model) renderFooter() string {
	spinner := m.ui.RenderSpinner()
	
	// 1. Sync Status / Spinner
	syncStatus := ""
	if spinner != "" {
		syncStatus = m.styles.Help.Render(spinner + " Syncing... ")
	}

	// 2. Last Sync Info
	lastSync := fmt.Sprintf("Last Sync: %s", humanize.Time(m.LastSyncAt))
	lastSyncStr := m.styles.Help.Render(lastSync)

	// 3. Quota Info (Subtle)
	quotaStr := ""
	if m.traffic != nil {
		quotaStr = m.styles.Help.Render(fmt.Sprintf(" [%d]", m.traffic.Remaining()))
	}

	footer := lipgloss.JoinHorizontal(lipgloss.Bottom, syncStatus, lastSyncStr, quotaStr)

	// Priority: Error only (status is now toasts)
	if m.err != nil {
		footer += " " + m.styles.StatusError.Render(fmt.Sprintf(" Error: %v ", m.err))
	}

	return footer
}

func (m *Model) renderMarkdown(text string) string {
	if m.markdownRenderer == nil {
		return text
	}

	out, err := m.markdownRenderer.Render(text)
	if err != nil {
		return text
	}
	return out
}

func (m *Model) updateMarkdownRenderer() {
	style := "dark"
	if !m.isDark {
		style = "light"
	}

	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(style),
		glamour.WithEmoji(),
		glamour.WithWordWrap(m.width-10),
	)
	if err == nil {
		m.markdownRenderer = r
	}
}
