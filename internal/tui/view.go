package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/glamour"
	"github.com/hirakiuc/gh-orbit/internal/types"
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
		status = fmt.Sprintf("Quota: %d", m.traffic.Remaining())
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
	case types.StatusUnsupported:
		bridge = "[FALLBACK]"
		bridgeStyle = m.styles.PriorityMed
	case types.StatusPermissionsDenied:
		bridge = "[NO PERMS]"
		bridgeStyle = m.styles.StatusError
	case types.StatusBroken:
		bridge = "[BROKEN]"
		bridgeStyle = m.styles.StatusError
	case types.StatusUnknown:
		bridge = "[PROBING]"
		bridgeStyle = m.styles.Help
	}

	health := bridgeStyle.Render(bridge)

	// 4. Rate Limit Status
	rlStatus := ""
	if m.RateLimit.Limit > 0 {
		threshold := int64(m.RateLimit.Limit) / 10
		if threshold > 1000 {
			threshold = 1000
		}

		if int64(m.RateLimit.Remaining) < threshold {
			diff := time.Until(m.RateLimit.Reset)
			mins := int(diff.Minutes()) + 1
			if mins <= 1 {
				rlStatus = m.styles.PriorityMed.Render(" [!] QUOTA LOW (<1m) ")
			} else {
				rlStatus = m.styles.PriorityMed.Render(fmt.Sprintf(" [!] QUOTA LOW (~%dm) ", mins))
			}
		}
	}

	// 5. Version Information
	vStr := m.styles.SelectedDescription.Render(" " + m.version + " ")

	footer := lipgloss.JoinHorizontal(
		lipgloss.Bottom,
		statusMsg,
		" ",
		filters,
		" ",
		rlStatus,
		lipgloss.PlaceHorizontal(m.width-lipgloss.Width(statusMsg)-lipgloss.Width(filters)-lipgloss.Width(rlStatus)-lipgloss.Width(bridge)-lipgloss.Width(vStr)-6, lipgloss.Right, vStr+" "+health),
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

	decision := ""
	if i.notification.ReviewDecision != "" {
		decisionStyle := m.styles.Subscribed
		switch i.notification.ReviewDecision {
		case "APPROVED":
			decisionStyle = m.styles.Assign
		case "CHANGES_REQUESTED", "REVIEW_REQUIRED":
			decisionStyle = m.styles.ActionRequired
		}
		decision = " • " + decisionStyle.Render(strings.ReplaceAll(i.notification.ReviewDecision, "_", " "))
	}

	meta := fmt.Sprintf("%s • %s • %s%s",
		i.notification.RepositoryFullName,
		i.notification.AuthorLogin,
		i.notification.ResourceState,
		decision)

	header := lipgloss.JoinVertical(lipgloss.Left, title, m.styles.SelectedDescription.Render(meta))

	// 2. Viewport (Body)
	// Dynamically calculate height based on header size
	detailMetaHeight := lipgloss.Height(header) + 1 // +1 for the newline

	availableHeight := m.height - m.headerHeight - m.footerHeight - detailMetaHeight
	if availableHeight < 5 {
		availableHeight = 5
	}

	// Sync viewport dimensions dynamically
	m.detailView.viewport.SetWidth(m.width - 4)
	m.detailView.viewport.SetHeight(availableHeight)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		"\n",
		m.detailView.viewport.View(),
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
