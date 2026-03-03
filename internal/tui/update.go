package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/db"
)

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		m.logger.Debug("Key pressed", "key", msg.String())
		// Temporary debug status
		// m.status = fmt.Sprintf("Key: [%s]", msg.String())
		if m.showDetail {
			if key.Matches(msg, m.keys.ToggleDetail) || msg.String() == "esc" {
				m.showDetail = false
				return m, nil
			}
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}

		// Only handle custom keys if the list isn't filtering
		if m.list.FilterState() == list.Filtering {
			break
		}

		switch {
		case msg.String() == "ctrl+c" || msg.String() == "q":
			return m, tea.Quit
		case key.Matches(msg, m.keys.Sync):
			if m.syncing {
				return m, nil
			}
			m.syncing = true
			return m, tea.Batch(
				m.syncNotifications(),
				m.spinner.Tick,
			)
		case key.Matches(msg, m.keys.ToggleDetail):
			m.logger.Debug("ToggleDetail key pressed")
			if i, ok := m.list.SelectedItem().(item); ok {
				m.showDetail = true
				if !i.notification.IsEnriched {
					m.fetchingDetail = true
					m.status = "Fetching details..."
					return m, tea.Batch(
						m.FetchDetailCmd(i.notification.GitHubID, i.notification.SubjectURL, i.notification.SubjectType),
						m.spinner.Tick,
					)
				}
				// Prepare viewport content
				m.status = "Showing detail"
				m.activeDetail = m.renderMarkdown(i.notification.Body)
				m.viewport.SetContent(m.activeDetail)
				return m, nil
			}
			m.status = "No item selected for detail"
			return m, nil
		case key.Matches(msg, m.keys.CopyURL):
			if i, ok := m.list.SelectedItem().(item); ok && i.notification.HTMLURL != "" {
				if isValidGitHubURL(i.notification.HTMLURL) {
					m.status = "Copied URL to clipboard"
					return m, tea.Batch(
						tea.SetClipboard(i.notification.HTMLURL),
						m.clearStatusAfter(3*time.Second),
					)
				}
				m.err = fmt.Errorf("refusing to copy untrusted URL: %s", i.notification.HTMLURL)
			}
		case key.Matches(msg, m.keys.ToggleRead):
			if i, ok := m.list.SelectedItem().(item); ok {
				cmds = append(cmds, m.ToggleRead(i))
			}
		case key.Matches(msg, m.keys.NextTab):
			m.activeTab = (m.activeTab + 1) % 4
			m.applyFilters()
		case key.Matches(msg, m.keys.PrevTab):
			m.activeTab = (m.activeTab - 1 + 4) % 4
			m.applyFilters()
		case key.Matches(msg, m.keys.OpenBrowser):
			if i, ok := m.list.SelectedItem().(item); ok && i.notification.HTMLURL != "" {
				m.status = "Opening browser..."
				return m, m.OpenBrowser(i.notification.HTMLURL)
			}
		case key.Matches(msg, m.keys.CheckoutPR):
			if i, ok := m.list.SelectedItem().(item); ok && i.notification.SubjectType == "PullRequest" {
				prNumber := extractNumberFromURL(i.notification.SubjectURL)
				if prNumber != "" {
					m.status = "Launching gh pr checkout..."
					return m, m.CheckoutPR(i.notification.RepositoryFullName, prNumber)
				}
				m.err = fmt.Errorf("could not extract PR number from URL: %s", i.notification.SubjectURL)
			}
		case key.Matches(msg, m.keys.ViewContextual):
			if i, ok := m.list.SelectedItem().(item); ok {
				return m, m.ViewItem(i)
			}
		case key.Matches(msg, m.keys.SetPriorityLow):
			return m, m.setPriority(1)
		case key.Matches(msg, m.keys.SetPriorityMed):
			return m, m.setPriority(2)
		case key.Matches(msg, m.keys.SetPriorityHigh):
			return m, m.setPriority(3)
		case key.Matches(msg, m.keys.ClearPriority):
			return m, m.setPriority(0)
		}

	case tea.WindowSizeMsg:
		m.list.SetSize(msg.Width, msg.Height-3) // 1 Header, 1 Tab bar, 1 Footer
		
		vpWidth := msg.Width - 4
		if vpWidth < 0 { vpWidth = 0 }
		m.viewport.SetWidth(vpWidth)
		
		vpHeight := msg.Height - 8
		if vpHeight < 0 { vpHeight = 0 }
		m.viewport.SetHeight(vpHeight)

		m.updateMarkdownRenderer()

	case tea.BackgroundColorMsg:
		m.isDark = msg.IsDark()
		m.styles = DefaultStyles(m.isDark)
		m.list.Styles.Title = m.styles.Title
		m.list.SetDelegate(newItemDelegate(m.styles, m.keys))
		m.updateMarkdownRenderer()

	case notificationsLoadedMsg:
		m.allNotifications = msg
		m.applyFilters()

	case syncCompleteMsg:
		m.syncing = false
		m.allNotifications = msg
		m.applyFilters()

	case detailLoadedMsg:
		m.fetchingDetail = false
		// Update master copy
		for idx, n := range m.allNotifications {
			if n.GitHubID == msg.GitHubID {
				m.allNotifications[idx].Body = msg.Body
				m.allNotifications[idx].AuthorLogin = msg.Author
				m.allNotifications[idx].HTMLURL = msg.HTMLURL
				m.allNotifications[idx].IsEnriched = true
				break
			}
		}
		m.activeDetail = m.renderMarkdown(msg.Body)
		m.viewport.SetContent(m.activeDetail)
		m.applyFilters()

	case spinner.TickMsg:
		if m.syncing || m.fetchingDetail {
			m.spinner, cmd = m.spinner.Update(msg)
			cmds = append(cmds, cmd)
		}

	case actionCompleteMsg:
		m.status = ""
		m.err = nil

	case clearStatusMsg:
		m.status = ""

	case errMsg:
		m.syncing = false
		m.err = msg.err
	}

	// Only update list if it's not filtering
	if m.list.FilterState() != list.Filtering {
		m.list, cmd = m.list.Update(msg)
		cmds = append(cmds, cmd)
	} else {
		// Even if filtering, we need to pass the message to the list
		// but bubbles/v2/list handles it internally in its Update.
		m.list, cmd = m.list.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) setPriority(priority int) tea.Cmd {
	if i, ok := m.list.SelectedItem().(item); ok {
		// Toggle logic: if already at this priority, reset to 0
		if i.notification.Priority == priority {
			priority = 0
		}

		i.notification.Priority = priority
		err := m.db.UpdateOrbitState(db.OrbitState{
			NotificationID: i.notification.GitHubID,
			Priority:       priority,
			Status:         i.notification.Status,
			IsReadLocally:  i.notification.IsReadLocally,
		})
		if err != nil {
			m.err = err
		}

		// Update feedback
		msg := "Priority cleared"
		switch priority {
		case 1:
			msg = "Priority set to Low"
		case 2:
			msg = "Priority set to Medium"
		case 3:
			msg = "Priority set to High"
		}
		m.status = msg

		// Update allNotifications master copy
		for idx, n := range m.allNotifications {
			if n.GitHubID == i.notification.GitHubID {
				m.allNotifications[idx].Priority = priority
				break
			}
		}
		m.applyFilters()

		return m.clearStatusAfter(3 * time.Second)
	}
	return nil
}

func (m *Model) applyFilters() {
	// 1. Store currently selected ID
	var selectedID string
	if i, ok := m.list.SelectedItem().(item); ok {
		selectedID = i.notification.GitHubID
	}

	// 2. Filter allNotifications
	var filtered []list.Item
	for _, n := range m.allNotifications {
		keep := false
		switch m.activeTab {
		case TabInbox:
			// Unread OR has priority
			if !n.IsReadLocally || n.Priority > 0 {
				keep = true
			}
		case TabUnread:
			if !n.IsReadLocally {
				keep = true
			}
		case TabTriaged:
			if n.Priority > 0 {
				keep = true
			}
		case TabAll:
			keep = true
		}

		if keep {
			filtered = append(filtered, item{notification: n})
		}
	}

	// 3. Update list items
	m.list.SetItems(filtered)

	// 4. Restore selection
	if selectedID != "" {
		for index, li := range m.list.Items() {
			if i, ok := li.(item); ok && i.notification.GitHubID == selectedID {
				m.list.Select(index)
				break
			}
		}
	}
}

func (m *Model) clearStatusAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(_ time.Time) tea.Msg {
		return clearStatusMsg{}
	})
}

func isValidGitHubURL(url string) bool {
	return strings.HasPrefix(url, "https://github.com/") ||
		strings.HasPrefix(url, "https://api.github.com/")
}
