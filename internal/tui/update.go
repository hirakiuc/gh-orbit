package tui

import (
	"context"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	"charm.land/lipgloss/v2"
	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/api"
	"github.com/hirakiuc/gh-orbit/internal/types"
)

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	// 1. Handle state-independent messages
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ui.SetSize(msg.Width, msg.Height)
		
		m.headerHeight = lipgloss.Height(m.renderHeader())
		m.footerHeight = lipgloss.Height(m.renderFooter())
		availableHeight := m.height - m.headerHeight - m.footerHeight

		m.listView.list.SetSize(msg.Width, availableHeight)
		
		m.detailView.viewport.SetWidth(msg.Width - 4)
		m.detailView.viewport.SetHeight(availableHeight - 2) // Reclaim space in detail view too
		m.updateMarkdownRenderer()

	case tea.BackgroundColorMsg:
		m.isDark = msg.IsDark()
		m.styles = DefaultStyles(m.isDark)
		m.listView.list.Styles.Title = m.styles.Title
		m.listView.delegate = newItemDelegate(m.styles, m.keys)
		m.listView.list.SetDelegate(m.listView.delegate)
		m.updateMarkdownRenderer()
		m.ui.SetStyles(m.styles)

	case notificationsLoadedMsg:
		m.allNotifications = msg.notifications
		m.applyFilters()
		// Start initial heartbeat and enrichment
		cmds = append(cmds, m.enrichViewport())
		cmds = append(cmds, m.tickHeartbeat())

	case priorityUpdatedMsg:
		m.allNotifications = msg.notifications
		m.applyFilters()
		cmds = append(cmds, m.ui.SetToast(msg.toast))

	case syncCompleteMsg:
		m.ui.SetSyncing(false)
		m.traffic.UpdateRateLimit(context.Background(), msg.remainingRateLimit)
		cmds = append(cmds, m.loadNotifications())
		m.LastSyncAt = time.Now()

	case detailLoadedMsg:
		m.ui.fetchingDetail = false
		// Update master copy
		for idx, n := range m.allNotifications {
			if n.GitHubID == msg.GitHubID {
				m.allNotifications[idx].Body = msg.Body
				m.allNotifications[idx].AuthorLogin = msg.Author
				m.allNotifications[idx].HTMLURL = msg.HTMLURL
				m.allNotifications[idx].ResourceState = msg.ResourceState
				m.allNotifications[idx].IsEnriched = true
				break
			}
		}
		// If we are in detail view and this is the active item, update viewport
		if m.state == StateDetail {
			if i, ok := m.listView.list.SelectedItem().(item); ok && i.notification.GitHubID == msg.GitHubID {
				m.detailView.activeDetail = m.renderMarkdown(msg.Body)
				m.detailView.viewport.SetContent(m.detailView.activeDetail)
			}
		}
		m.applyFilters()

	case pollTickMsg:
		if msg.ID == m.heartbeatID {
			if time.Since(m.LastSyncAt).Seconds() >= float64(m.PollInterval) {
				cmds = append(cmds, m.syncNotificationsWithForce(false))
				m.ui.SetSyncing(true)
			}
			cmds = append(cmds, m.tickHeartbeat())
		}

	case clockTickMsg:
		if msg.ID == m.clockID {
			cmds = append(cmds, m.tickClock())
		}

	case viewportEnrichMsg:
		cmds = append(cmds, m.enrichViewport())

	case spinner.TickMsg:
		m.ui, cmd = m.ui.Update(msg)
		cmds = append(cmds, cmd)

	case clearStatusMsg:
		m.ui, cmd = m.ui.Update(msg)
		cmds = append(cmds, cmd)

	case errMsg:
		m.err = msg.err
		m.ui.SetSyncing(false)
		m.ui.fetchingDetail = false
	}

	// 2. Handle state-dependent messages (Router Pattern)
	var stateCmd tea.Cmd
	oldIndex := m.listView.list.Index()
	
	switch m.state {
	case StateDetail:
		_, stateCmd = m.updateDetail(msg)
	case StateList:
		_, stateCmd = m.updateList(msg)
	}
	cmds = append(cmds, stateCmd)

	// Trigger debounced enrichment if viewport might have changed
	if m.state == StateList && m.listView.list.Index() != oldIndex {
		duration := 250 * time.Millisecond // Default debounce
		cmds = append(cmds, tea.Tick(duration, func(_ time.Time) tea.Msg {
			return viewportEnrichMsg{}
		}))
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) updateDetail(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		if key.Matches(msg, m.keys.ToggleDetail) || msg.String() == "esc" || msg.String() == "q" {
			m.state = StateList
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.detailView.viewport, cmd = m.detailView.viewport.Update(msg)
	return m, cmd
}

func (m *Model) updateList(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Only handle custom keys if the list isn't filtering
		if m.listView.list.FilterState() == list.Filtering {
			break
		}

		switch {
		case msg.String() == "ctrl+c":
			return m, tea.Quit
		case msg.String() == "q":
			if m.listView.list.Help.ShowAll {
				// Bubble Tea v2 list handles its own help toggle
				break
			}
			return m, tea.Quit
		case key.Matches(msg, m.keys.Sync):
			if !m.ui.syncing {
				cmds = append(cmds, m.syncNotificationsWithForce(true))
				m.ui.SetSyncing(true)
			}
		case key.Matches(msg, m.keys.ToggleDetail):
			if i, ok := m.listView.list.SelectedItem().(item); ok {
				m.state = StateDetail
				if !i.notification.IsEnriched {
					m.ui.fetchingDetail = true
					cmds = append(cmds, m.FetchDetailCmd(i.notification.GitHubID, i.notification.SubjectURL, i.notification.SubjectType))
				} else {
					m.detailView.viewport.SetContent(m.detailView.activeDetail)
				}
			}
		case key.Matches(msg, m.keys.ToggleRead):
			if i, ok := m.listView.list.SelectedItem().(item); ok {
				cmds = append(cmds, m.ToggleRead(i))
			}
		case key.Matches(msg, m.keys.NextTab):
			m.listView.activeTab = (m.listView.activeTab + 1) % 4
			m.applyFilters()
		case key.Matches(msg, m.keys.PrevTab):
			m.listView.activeTab = (m.listView.activeTab - 1 + 4) % 4
			m.applyFilters()
		case key.Matches(msg, m.keys.OpenBrowser):
			if i, ok := m.listView.list.SelectedItem().(item); ok {
				cmds = append(cmds, m.ViewItem(i))
			}
		case key.Matches(msg, m.keys.CheckoutPR):
			if i, ok := m.listView.list.SelectedItem().(item); ok && i.notification.SubjectType == "PullRequest" {
				number := extractNumberFromURL(i.notification.SubjectURL)
				if number != "" {
					cmds = append(cmds, m.CheckoutPR(i.notification.RepositoryFullName, number))
				}
			}
		case key.Matches(msg, m.keys.SetPriorityLow):
			cmds = append(cmds, m.setPriority(1))
		case key.Matches(msg, m.keys.SetPriorityMed):
			cmds = append(cmds, m.setPriority(2))
		case key.Matches(msg, m.keys.SetPriorityHigh):
			cmds = append(cmds, m.setPriority(3))
		case key.Matches(msg, m.keys.ClearPriority):
			cmds = append(cmds, m.setPriority(0))
		case key.Matches(msg, m.keys.FilterPR):
			m.toggleResourceFilter("PullRequest", "PRs")
		case key.Matches(msg, m.keys.FilterIssue):
			m.toggleResourceFilter("Issue", "Issues")
		case key.Matches(msg, m.keys.FilterDiscussion):
			m.toggleResourceFilter("Discussion", "Discussions")
		case msg.String() == "1", msg.String() == "2", msg.String() == "3", msg.String() == "4":
			m.listView.activeTab = int(msg.String()[0]-'1')
			m.applyFilters()
		}
	}

	m.listView.list, cmd = m.listView.list.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m *Model) enrichViewport() tea.Cmd {
	// Identify visible items
	start, end := m.listView.list.Paginator.GetSliceBounds(len(m.listView.list.Items()))
	if start < 0 || end > len(m.listView.list.Items()) || start >= end {
		return nil
	}
	visible := m.listView.list.Items()[start:end]

	var toEnrich []types.NotificationWithState
	for _, li := range visible {
		if i, ok := li.(item); ok {
			var isExpired bool
			if i.notification.IsEnriched {
				if i.notification.EnrichedAt.Valid {
					isExpired = time.Since(i.notification.EnrichedAt.Time) > api.StatusTTL
				} else {
					isExpired = true
				}
			}

			if !i.notification.IsEnriched || isExpired {
				toEnrich = append(toEnrich, i.notification)
			}
		}
	}

	if len(toEnrich) == 0 {
		return nil
	}

	return m.traffic.Submit(api.PriorityEnrich, func(ctx context.Context) tea.Msg {
		results := m.enrich.FetchHybridBatch(ctx, toEnrich)
		if len(results) == 0 {
			return nil
		}

		notifs, err := m.db.ListNotifications(ctx)
		if err != nil {
			return errMsg{err: err}
		}
		return notificationsLoadedMsg{notifications: notifs}
	})
}

func (m *Model) setPriority(priority int) tea.Cmd {
	if i, ok := m.listView.list.SelectedItem().(item); ok {
		// Toggle logic
		if i.notification.Priority == priority {
			priority = 0
		}

		return m.traffic.Submit(api.PriorityUser, func(ctx context.Context) tea.Msg {
			err := m.db.SetPriority(ctx, i.notification.GitHubID, priority)
			if err != nil {
				return errMsg{err: err}
			}

			// Reload to reflect state
			notifs, err := m.db.ListNotifications(ctx)
			if err != nil {
				return errMsg{err: err}
			}
			
			toast := "Priority cleared"
			switch priority {
			case 1: toast = "Priority set to Low"
			case 2: toast = "Priority set to Medium"
			case 3: toast = "Priority set to High"
			}

			return priorityUpdatedMsg{notifications: notifs, toast: toast}
		})
	}
	return nil
}

func (m *Model) applyFilters() {
	var selectedID string
	if i, ok := m.listView.list.SelectedItem().(item); ok {
		selectedID = i.notification.GitHubID
	}

	var filtered []list.Item
	for _, n := range m.allNotifications {
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

		if keep && m.listView.resourceFilter != "" {
			if n.SubjectType != m.listView.resourceFilter {
				keep = false
			}
		}

		if keep {
			filtered = append(filtered, item{notification: n})
		}
	}

	m.listView.list.SetItems(filtered)

	if selectedID != "" {
		for index, li := range m.listView.list.Items() {
			if i, ok := li.(item); ok && i.notification.GitHubID == selectedID {
				m.listView.list.Select(index)
				break
			}
		}
	}
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
