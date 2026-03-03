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

	if m.showDetail {
		if i, ok := m.list.SelectedItem().(item); ok {
			windowTitle = fmt.Sprintf("gh-orbit: %s", i.notification.SubjectTitle)
		}
		viewContent = m.renderDetailView()
	} else {
		// Build the view content from components
		viewContent = m.renderTabs()
		viewContent += "\n" + m.renderList()
	}

	footer := m.renderFooter()
	if footer != "" {
		// We add a newline if the footer is fixed, 
		// but since we are moving status to toasts, 
		// the footer only contains the spinner/sync state now.
		viewContent += "\n" + footer
	}

	v := tea.NewView(viewContent)

	// Overlays via Canvas
	if m.width > 0 && m.height > 0 {
		canvas := lipgloss.NewCanvas(m.width, m.height)
		
		// Base layer
		base := lipgloss.NewLayer(viewContent).X(0).Y(0).Z(0)
		canvas.Compose(base)

		// 1. Toast Notification
		if m.toastMessage != "" {
			toast := m.styles.Toast.Render(m.toastMessage)
			// Place toast at the bottom center
			toastWidth := lipgloss.Width(toast)
			toastY := m.height - 2
			if toastY < 0 { toastY = 0 }
			
			layer := lipgloss.NewLayer(toast).
				X((m.width - toastWidth) / 2).
				Y(toastY).
				Z(100)
			canvas.Compose(layer)
		}

		// 2. Scrollbar for Detail View
		if m.showDetail && !m.fetchingDetail {
			percent := m.viewport.ScrollPercent()
			if percent >= 0 {
				scrollbarHeight := m.viewport.Height()
				totalLines := m.viewport.TotalLineCount()
				
				thumbHeight := 3 // Minimal thumb size
				if totalLines > 0 {
					thumbHeight = int(float64(scrollbarHeight) * (float64(scrollbarHeight) / float64(totalLines)))
					if thumbHeight < 3 { thumbHeight = 3 }
				}
				if thumbHeight > scrollbarHeight { thumbHeight = scrollbarHeight }
				
				thumbPos := int(float64(scrollbarHeight-thumbHeight) * percent)
				
				thumb := m.styles.ScrollbarThumb.
					Width(1).
					Height(thumbHeight).
					Render(" ")
				
				// Place scrollbar on the right edge of the viewport
				layer := lipgloss.NewLayer(thumb).
					X(m.width - 2).
					Y(4 + thumbPos). // Start after header
					Z(50)
				canvas.Compose(layer)
			}
		}

		v = tea.NewView(canvas.Render())
	}

	// Declarative terminal state
	v.AltScreen = true
	v.WindowTitle = windowTitle
	v.MouseMode = tea.MouseModeCellMotion

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
