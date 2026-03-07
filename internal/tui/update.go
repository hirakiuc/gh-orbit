package tui

import (
	"context"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/api"
)

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		s := msg.String()
		switch s {
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			if m.state == StateDetail {
				m.state = StateList
				return m, nil
			}
			if m.listView.list.FilterState() == list.Filtering {
				break
			}
			return m, tea.Quit
		case "r":
			if i, ok := m.listView.list.SelectedItem().(item); ok {
				cmds = append(cmds, m.ToggleRead(i))
			}
		case "s":
			cmds = append(cmds, m.syncNotificationsWithForce(true))
			m.ui.SetSyncing(true)
		case "enter":
			if m.state == StateList {
				if i, ok := m.listView.list.SelectedItem().(item); ok {
					m.state = StateDetail
					m.detailView.activeDetail = i.notification.Body
					if !i.notification.IsEnriched {
						m.ui.fetchingDetail = true
						cmds = append(cmds, m.FetchDetailCmd(i.notification.GitHubID, i.notification.SubjectURL, i.notification.SubjectType))
					}
				}
			}
		case "o":
			if i, ok := m.listView.list.SelectedItem().(item); ok {
				cmds = append(cmds, m.ViewItem(i))
			}
		case "c":
			if i, ok := m.listView.list.SelectedItem().(item); ok {
				if i.notification.SubjectType == "PullRequest" {
					number := extractNumberFromURL(i.notification.SubjectURL)
					if number != "" {
						cmds = append(cmds, m.CheckoutPR(i.notification.RepositoryFullName, number))
					}
				}
			}
		case "tab":
			m.listView.activeTab = (m.listView.activeTab + 1) % 4
			m.applyFilters()
		case "1", "2", "3", "4":
			m.listView.activeTab = int(s[0]-'1')
			m.applyFilters()
		case "p":
			// Toggle resource filter (PRs only)
			m.toggleResourceFilter("PullRequest", "PRs")
		case "i":
			// Toggle resource filter (Issues only)
			m.toggleResourceFilter("Issue", "Issues")
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ui.SetSize(msg.Width, msg.Height)
		m.listView.list.SetSize(msg.Width, msg.Height-6)
		
		// v2: Viewport methods for dimension updates (pointer receiver)
		m.detailView.viewport.SetWidth(msg.Width - 4)
		m.detailView.viewport.SetHeight(msg.Height - 8)
		m.updateMarkdownRenderer()

	case notificationsLoadedMsg:
		m.allNotifications = msg.notifications
		m.applyFilters()

	case syncCompleteMsg:
		m.ui.SetSyncing(false)
		m.traffic.UpdateRateLimit(context.Background(), msg.remainingRateLimit)
		cmds = append(cmds, m.loadNotifications())

	case detailLoadedMsg:
		m.ui.fetchingDetail = false
		// Update the item in the master list
		for i, n := range m.allNotifications {
			if n.GitHubID == msg.GitHubID {
				m.allNotifications[i].Body = msg.Body
				m.allNotifications[i].AuthorLogin = msg.Author
				m.allNotifications[i].HTMLURL = msg.HTMLURL
				m.allNotifications[i].ResourceState = msg.ResourceState
				m.allNotifications[i].IsEnriched = true
				break
			}
		}
		// If the selected item is the one we just loaded, update viewport
		if i, ok := m.listView.list.SelectedItem().(item); ok && i.notification.GitHubID == msg.GitHubID {
			m.detailView.activeDetail = msg.Body
		}
		m.applyFilters()

	case pollTickMsg:
		if time.Since(m.LastSyncAt).Seconds() >= float64(m.PollInterval) {
			cmds = append(cmds, m.syncNotificationsWithForce(false))
			m.ui.SetSyncing(true)
			m.LastSyncAt = time.Now()
		}
		cmds = append(cmds, m.tickHeartbeat())

	case clockTickMsg:
		cmds = append(cmds, m.tickClock())

	case clearStatusMsg:
		m.ui.SetToast("")

	case tea.BackgroundColorMsg:
		// Detect dark/light mode and update styles
		// This is a simplified example
		isDark := true // detect from msg.Color
		m.isDark = isDark
		m.styles = DefaultStyles(isDark)
		m.updateMarkdownRenderer()

	case errMsg:
		m.err = msg.err
		m.ui.SetSyncing(false)
		m.ui.fetchingDetail = false
	}

	// Update sub-models
	var cmd tea.Cmd
	if m.state == StateList {
		m.listView.list, cmd = m.listView.list.Update(msg)
		cmds = append(cmds, cmd)
	} else {
		m.detailView.viewport, cmd = m.detailView.viewport.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) applyFilters() {
	var filtered []list.Item
	
	for _, n := range m.allNotifications {
		// 1. Tab Filter
		keep := false
		switch m.listView.activeTab {
		case TabInbox:
			keep = !n.IsReadLocally && n.Status != "archived"
		case TabUnread:
			keep = !n.IsReadLocally
		case TabTriaged:
			keep = n.Priority > 0
		case TabAll:
			keep = true
		}

		if !keep {
			continue
		}

		// 2. Resource Type Filter
		if m.listView.resourceFilter != "" && n.SubjectType != m.listView.resourceFilter {
			continue
		}

		filtered = append(filtered, item{notification: n})
	}

	m.listView.list.SetItems(filtered)
}

func (m *Model) toggleResourceFilter(resType, label string) {
	if m.listView.resourceFilter == resType {
		m.listView.resourceFilter = ""
		m.ui.SetResourceFilter("")
	} else {
		m.listView.resourceFilter = resType
		m.ui.SetResourceFilter(label)
	}
	m.applyFilters()
}

// UpdateNotification is a helper to refresh a single notification's state from DB
func (m *Model) refreshNotification(id string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		notifs, err := m.db.ListNotifications(ctx)
		if err != nil {
			return errMsg{err: err}
		}
		return notificationsLoadedMsg{notifications: notifs}
	}
}

// SetPriority handles the manual priority assignment
func (m *Model) SetPriority(id string, priority int) tea.Cmd {
	return m.traffic.Submit(api.PriorityUser, func(ctx context.Context) tea.Msg {
		err := m.db.SetPriority(ctx, id, priority)
		if err != nil {
			return errMsg{err: err}
		}
		
		// Reload notifications to update UI
		notifs, _ := m.db.ListNotifications(ctx)
		return notificationsLoadedMsg{notifications: notifs}
	})
}
