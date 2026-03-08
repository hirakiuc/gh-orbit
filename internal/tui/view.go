package tui

import (
	"fmt"

	"charm.land/lipgloss/v2"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/glamour"
	"github.com/hirakiuc/gh-orbit/internal/api"
)

func (m *Model) View() tea.View {
	if m.err != nil {
		return tea.View{
			Content:   m.styles.StatusError.Render(fmt.Sprintf("Error: %v", m.err)),
			AltScreen: true,
		}
	}

	var content string
	switch m.state {
	case StateDetail:
		content = m.renderDetailView()
	case StateList:
		content = m.listView.list.View()
	}

	rendered := lipgloss.JoinVertical(
		lipgloss.Left,
		m.renderHeader(),
		content,
		m.renderFooter(),
	)

	return tea.View{
		Content:   rendered,
		AltScreen: true,
	}
}

func (m *Model) renderHeader() string {
	// 1. Current Tab Bar
	tabs := []string{" Inbox ", " Unread ", " Triaged ", " All "}
	var renderedTabs []string

	for i, t := range tabs {
		if i == m.listView.activeTab {
			renderedTabs = append(renderedTabs, m.styles.TabActive.Render(t))
		} else {
			renderedTabs = append(renderedTabs, m.styles.TabInactive.Render(t))
		}
	}

	tabBar := lipgloss.JoinHorizontal(lipgloss.Top, renderedTabs...)
	
	// 2. Right-side Status (Rate Limit / Sync)
	status := ""
	if m.ui.syncing {
		status = m.ui.spinner.View() + " Syncing..."
	} else {
		status = fmt.Sprintf("Rate: %d", m.traffic.Remaining())
	}

	header := lipgloss.JoinHorizontal(
		lipgloss.Left,
		tabBar,
		lipgloss.PlaceHorizontal(m.width-lipgloss.Width(tabBar), lipgloss.Right, status),
	)

	return m.styles.TabContainer.Render(header)
}

func (m *Model) renderFooter() string {
	// 1. Toast / Status Message
	statusMsg := ""
	if m.ui.toastMessage != "" {
		statusMsg = m.styles.Toast.Render(m.ui.toastMessage)
	}

	// 2. Active Filters
	filters := ""
	if m.ui.resourceFilter != "" {
		filters = m.styles.FilterChip.Render(" " + m.ui.resourceFilter + " ")
	}

	// 3. Bridge Health Indicator
	bridge := "[NATIVE]"
	bridgeStyle := m.styles.StatusNormal
	
	switch m.bridgeStatus {
	case api.StatusUnsupported:
		bridge = "[FALLBACK]"
		bridgeStyle = m.styles.PriorityMed
	case api.StatusPermissionsDenied:
		bridge = "[NO PERMS]"
		bridgeStyle = m.styles.StatusError
	case api.StatusBroken:
		bridge = "[BROKEN]"
		bridgeStyle = m.styles.StatusError
	case api.StatusUnknown:
		bridge = "[PROBING]"
		bridgeStyle = m.styles.Help
	}
	
	health := bridgeStyle.Render(bridge)

	footer := lipgloss.JoinHorizontal(
		lipgloss.Bottom,
		statusMsg,
		" ",
		filters,
		lipgloss.PlaceHorizontal(m.width-lipgloss.Width(statusMsg)-lipgloss.Width(filters)-lipgloss.Width(bridge)-2, lipgloss.Right, health),
	)

	return footer
}

func (m *Model) renderDetailView() string {
	i, ok := m.listView.list.SelectedItem().(item)
	if !ok {
		return "No item selected"
	}

	// 1. Header (Title + Metadata)
	title := m.styles.DetailHeader.Render(i.notification.SubjectTitle)
	
	meta := fmt.Sprintf("%s • %s • %s", 
		i.notification.RepositoryFullName, 
		i.notification.AuthorLogin,
		i.notification.ResourceState)
	
	header := lipgloss.JoinVertical(lipgloss.Left, title, m.styles.SelectedDescription.Render(meta))

	// 2. Viewport (Body)
	detailMetaHeight := lipgloss.Height(header) + 1 // +1 for the newline
	
	availableHeight := m.height - m.headerHeight - m.footerHeight - detailMetaHeight
	if availableHeight < 5 { availableHeight = 5 }

	body := m.detailView.activeDetail
	if m.ui.fetchingDetail {
		body = "\n  ◌ Loading content..."
	} else if body == "" {
		body = "\n  (No description provided)"
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		"\n",
		m.styles.Viewport.Width(m.width-4).Height(availableHeight).Render(body),
	)
}

func (m *Model) renderMarkdown(content string) string {
	if content == "" {
		return ""
	}

	out, err := m.markdownRenderer.Render(content)
	if err != nil {
		return content
	}
	return out
}

func (m *Model) updateMarkdownRenderer() {
	style := "dark"
	if !m.isDark {
		style = "light"
	}

	r, _ := glamour.NewTermRenderer(
		glamour.WithStandardStyle(style),
		glamour.WithWordWrap(m.width-10),
	)
	m.markdownRenderer = r
}
