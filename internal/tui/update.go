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

	// 1. Handle state-independent messages
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.list.SetSize(msg.Width, msg.Height-3)
		
		vpWidth := msg.Width - 4
		if vpWidth < 0 { vpWidth = 0 }
		m.viewport.SetWidth(vpWidth)
		
		vpHeight := msg.Height - 8
		if vpHeight < 0 { vpHeight = 0 }
		m.viewport.SetHeight(vpHeight)

		m.updateMarkdownRenderer()
		m.ui.SetSize(msg.Width, msg.Height)

	case tea.BackgroundColorMsg:
		m.isDark = msg.IsDark()
		m.styles = DefaultStyles(m.isDark)
		m.list.Styles.Title = m.styles.Title
		m.list.SetDelegate(newItemDelegate(m.styles, m.keys))
		m.updateMarkdownRenderer()
		m.ui.SetStyles(m.styles)

	case notificationsLoadedMsg:
		m.allNotifications = msg
		m.applyFilters()

	case syncCompleteMsg:
		cmds = append(cmds, m.ui.SetSyncing(false))
		m.allNotifications = msg
		m.applyFilters()

	case detailLoadedMsg:
		cmds = append(cmds, m.ui.SetFetching(false))
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
		m.ui, cmd = m.ui.Update(msg)
		cmds = append(cmds, cmd)

	case clearStatusMsg:
		m.ui, cmd = m.ui.Update(msg)
		cmds = append(cmds, cmd)

	case errMsg:
		cmds = append(cmds, m.ui.SetSyncing(false))
		cmds = append(cmds, m.ui.SetFetching(false))
		m.err = msg.err
	}

	// 2. Handle state-dependent messages
	switch m.state {
	case StateDetail:
		if msg, ok := msg.(tea.KeyPressMsg); ok {
			if key.Matches(msg, m.keys.ToggleDetail) || msg.String() == "esc" {
				m.state = StateList
				return m, nil
			}
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}

	case StateList:
		switch msg := msg.(type) {
		case tea.KeyPressMsg:
			// Only handle custom keys if the list isn't filtering
			if m.list.FilterState() == list.Filtering {
				break
			}

			switch {
			case msg.String() == "ctrl+c" || msg.String() == "q":
				return m, tea.Quit
			case key.Matches(msg, m.keys.Sync):
				if m.ui.syncing {
					return m, nil
				}
				return m, tea.Batch(
					m.ui.SetSyncing(true),
					m.syncNotifications(),
				)
			case key.Matches(msg, m.keys.ToggleDetail):
				if i, ok := m.list.SelectedItem().(item); ok {
					m.state = StateDetail
					if !i.notification.IsEnriched {
						return m, tea.Batch(
							m.ui.SetFetching(true),
							m.FetchDetailCmd(i.notification.GitHubID, i.notification.SubjectURL, i.notification.SubjectType),
						)
					}
					// Prepare viewport content
					m.activeDetail = m.renderMarkdown(i.notification.Body)
					m.viewport.SetContent(m.activeDetail)
					return m, nil
				}
				return m, nil
			case key.Matches(msg, m.keys.CopyURL):
				if i, ok := m.list.SelectedItem().(item); ok && i.notification.HTMLURL != "" {
					if isValidGitHubURL(i.notification.HTMLURL) {
						return m, tea.Batch(
							tea.SetClipboard(i.notification.HTMLURL),
							m.ui.SetToast("Copied URL to clipboard"),
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
					cmds = append(cmds, m.ui.SetToast("Opening browser..."))
					return m, tea.Batch(m.OpenBrowser(i.notification.HTMLURL), tea.Batch(cmds...))
				}
			case key.Matches(msg, m.keys.CheckoutPR):
				if i, ok := m.list.SelectedItem().(item); ok && i.notification.SubjectType == "PullRequest" {
					prNumber := extractNumberFromURL(i.notification.SubjectURL)
					if prNumber != "" {
						cmds = append(cmds, m.ui.SetToast("Launching gh pr checkout..."))
						return m, tea.Batch(m.CheckoutPR(i.notification.RepositoryFullName, prNumber), tea.Batch(cmds...))
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
		}

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

		// Update allNotifications master copy
		for idx, n := range m.allNotifications {
			if n.GitHubID == i.notification.GitHubID {
				m.allNotifications[idx].Priority = priority
				break
			}
		}
		m.applyFilters()

		return m.ui.SetToast(msg)
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
