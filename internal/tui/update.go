package tui

import (
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
	// Global bridge status refresh (Imperative Shell side-effect)
	m.bridgeStatus = m.sync.BridgeStatus()

	// 1. Transition State (Functional Core)
	oldIndex := m.listView.list.Index()
	actions := m.Transition(msg, oldIndex)

	// 2. Execute Actions (Imperative Shell)
	var cmds []tea.Cmd
	for _, action := range actions {
		cmds = append(cmds, m.interpreter.Execute(action))
	}

	// 3. Handle sub-model updates that still use traditional TEA
	// Some sub-models (list, viewport) handle their own state internally.
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case spinner.TickMsg:
		m.ui, cmd = m.ui.Update(msg)
		cmds = append(cmds, cmd)
	case clearStatusMsg:
		m.ui, cmd = m.ui.Update(msg)
		cmds = append(cmds, cmd)
	}

	switch m.state {
	case StateDetail:
		m.detailView.viewport, cmd = m.detailView.viewport.Update(msg)
		cmds = append(cmds, cmd)
	case StateList:
		m.listView.list, cmd = m.listView.list.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) Transition(msg tea.Msg, oldIndex int) []Action {
	var actions []Action

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
		m.detailView.viewport.SetHeight(availableHeight - 2)
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
		if m.state == StateDetail {
			m.refreshDetailView()
		}
		if msg.IsInitial {
			actions = append(actions, ActionEnrichItems{Notifications: m.getVisibleNotifications()})
			actions = append(actions, ActionScheduleTick{TickType: TickHeartbeat, Interval: m.heartbeatInterval})
		}

	case priorityUpdatedMsg:
		m.allNotifications = msg.notifications
		m.applyFilters()
		actions = append(actions, ActionShowToast{Message: msg.toast})

	case syncCompleteMsg:
		m.ui.SetSyncing(false)
		// Rate limit update is an imperative effect, but tracked in model
		m.LastSyncAt = time.Now()
		m.RateLimit = msg.rateLimit
		actions = append(actions, ActionUpdateRateLimit{Info: msg.rateLimit})
		actions = append(actions, ActionLoadNotifications{}) // Reload after sync

	case detailLoadedMsg:
		m.ui.SetFetching(false)
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
		if m.state == StateDetail {
			m.applyFilters() // This updates the items in the list from the modified allNotifications
			m.refreshDetailView()
		} else {
			m.applyFilters()
		}

	case pollTickMsg:
		if msg.ID == m.heartbeatID {
			if time.Since(m.LastSyncAt).Seconds() >= float64(m.PollInterval) {
				actions = append(actions, ActionSyncNotifications{Force: false})
				m.ui.SetSyncing(true)
			}
			actions = append(actions, ActionScheduleTick{TickType: TickHeartbeat, Interval: m.heartbeatInterval})
		}

	case clockTickMsg:
		if msg.ID == m.clockID {
			actions = append(actions, ActionScheduleTick{TickType: TickClock, Interval: m.clockInterval})
		}

	case viewportEnrichMsg:
		actions = append(actions, ActionEnrichItems{Notifications: m.getVisibleNotifications()})

	case errMsg:
		m.err = msg.err
		m.ui.SetSyncing(false)
		m.ui.SetFetching(false)
	}

	// State-dependent transitions
	var stateActions []Action
	switch m.state {
	case StateDetail:
		stateActions = m.transitionDetail(msg)
	case StateList:
		stateActions = m.transitionList(msg)
	}
	actions = append(actions, stateActions...)

	// Debounced enrichment logic
	if m.state == StateList && m.listView.list.Index() != oldIndex {
		actions = append(actions, ActionScheduleTick{TickType: TickEnrich, Interval: 250 * time.Millisecond})
	}

	return actions
}

func (m *Model) transitionDetail(msg tea.Msg) []Action {
	var actions []Action
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.Matches(msg, m.keys.Back), key.Matches(msg, m.keys.ToggleDetail):
			m.state = StateList
		case key.Matches(msg, m.keys.OpenBrowser):
			if i, ok := m.listView.list.SelectedItem().(item); ok {
				actions = append(actions, ActionViewWeb{Notification: i.notification})
			}
		case key.Matches(msg, m.keys.CheckoutPR):
			if i, ok := m.listView.list.SelectedItem().(item); ok && i.notification.SubjectType == "PullRequest" {
				number := extractNumberFromURL(i.notification.SubjectURL)
				if number != "" {
					actions = append(actions, ActionCheckoutPR{Repository: i.notification.RepositoryFullName, Number: number})
				}
			}
		case key.Matches(msg, m.keys.Quit):
			return m.handleQuitTransition()
		}
	}
	return actions
}

func (m *Model) handleQuitTransition() []Action {
	if time.Since(m.lastQuitPress) < 500*time.Millisecond {
		return []Action{ActionQuit{}}
	}
	m.lastQuitPress = time.Now()
	return []Action{ActionShowToast{Message: "Press q again to quit"}}
}

func (m *Model) transitionList(msg tea.Msg) []Action {
	var actions []Action

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.listView.list.FilterState() == list.Filtering {
			break
		}

		switch {
		case msg.String() == "ctrl+c":
			actions = append(actions, ActionQuit{})
		case key.Matches(msg, m.keys.Quit):
			if !m.listView.list.Help.ShowAll {
				return m.handleQuitTransition()
			}
		case key.Matches(msg, m.keys.Sync):
			if !m.ui.syncing {
				actions = append(actions, ActionSyncNotifications{Force: true})
				m.ui.SetSyncing(true)
			}
		case key.Matches(msg, m.keys.ToggleDetail):
			if i, ok := m.listView.list.SelectedItem().(item); ok {
				m.state = StateDetail
				if !i.notification.IsEnriched {
					m.ui.SetFetching(true)
					actions = append(actions, ActionEnrichItems{Notifications: []types.NotificationWithState{i.notification}})
				}
				m.refreshDetailView()
			}
		case key.Matches(msg, m.keys.ToggleRead):
			if i, ok := m.listView.list.SelectedItem().(item); ok {
				newState := !i.notification.IsReadLocally
				actions = append(actions, ActionMarkRead{ID: i.notification.GitHubID, Read: newState})
				toast := "Marked as unread"
				if newState {
					toast = "Marked as read"
				}
				actions = append(actions, ActionShowToast{Message: toast})
			}
		case key.Matches(msg, m.keys.NextTab):
			m.listView.activeTab = (m.listView.activeTab + 1) % 4
			m.applyFilters()
		case key.Matches(msg, m.keys.PrevTab):
			m.listView.activeTab = (m.listView.activeTab - 1 + 4) % 4
			m.applyFilters()
		case key.Matches(msg, m.keys.Tab1):
			m.listView.activeTab = TabInbox
			m.applyFilters()
		case key.Matches(msg, m.keys.Tab2):
			m.listView.activeTab = TabUnread
			m.applyFilters()
		case key.Matches(msg, m.keys.Tab3):
			m.listView.activeTab = TabTriaged
			m.applyFilters()
		case key.Matches(msg, m.keys.Tab4):
			m.listView.activeTab = TabAll
			m.applyFilters()
		case key.Matches(msg, m.keys.OpenBrowser):
			if i, ok := m.listView.list.SelectedItem().(item); ok {
				actions = append(actions, ActionViewWeb{Notification: i.notification})
			}
		case key.Matches(msg, m.keys.CheckoutPR):
			if i, ok := m.listView.list.SelectedItem().(item); ok && i.notification.SubjectType == "PullRequest" {
				number := extractNumberFromURL(i.notification.SubjectURL)
				if number != "" {
					actions = append(actions, ActionCheckoutPR{Repository: i.notification.RepositoryFullName, Number: number})
				}
			}
		case key.Matches(msg, m.keys.FilterPR):
			m.toggleResourceFilter("PullRequest", "PRs")
		case key.Matches(msg, m.keys.FilterIssue):
			m.toggleResourceFilter("Issue", "Issues")
		case key.Matches(msg, m.keys.FilterDiscussion):
			m.toggleResourceFilter("Discussion", "Discussions")
		case key.Matches(msg, m.keys.PriorityUp):
			if i, ok := m.listView.list.SelectedItem().(item); ok {
				newP := (i.notification.Priority + 1) % 4
				actions = append(actions, ActionSetPriority{ID: i.notification.GitHubID, Priority: newP})
				actions = append(actions, ActionShowToast{Message: m.getPriorityToast(newP)})
			}
		case key.Matches(msg, m.keys.PriorityDown):
			if i, ok := m.listView.list.SelectedItem().(item); ok {
				newP := (i.notification.Priority - 1 + 4) % 4
				actions = append(actions, ActionSetPriority{ID: i.notification.GitHubID, Priority: newP})
				actions = append(actions, ActionShowToast{Message: m.getPriorityToast(newP)})
			}
		case key.Matches(msg, m.keys.PriorityNone):
			if i, ok := m.listView.list.SelectedItem().(item); ok {
				actions = append(actions, ActionSetPriority{ID: i.notification.GitHubID, Priority: 0})
				actions = append(actions, ActionShowToast{Message: "Priority cleared"})
			}
		}
	}

	return actions
}

func (m *Model) getPriorityToast(p int) string {
	switch p {
	case 1:
		return "Priority set to Low"
	case 2:
		return "Priority set to Medium"
	case 3:
		return "Priority set to High"
	default:
		return "Priority cleared"
	}
}

func (m *Model) getVisibleNotifications() []types.NotificationWithState {
	start, end := m.listView.list.Paginator.GetSliceBounds(len(m.listView.list.Items()))
	if start < 0 || end > len(m.listView.list.Items()) || start >= end {
		return nil
	}
	visible := m.listView.list.Items()[start:end]

	var items []types.NotificationWithState
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
				items = append(items, i.notification)
			}
		}
	}
	return items
}

func (m *Model) refreshDetailView() {
	if i, ok := m.listView.list.SelectedItem().(item); ok {
		if i.notification.IsEnriched {
			m.detailView.activeDetail = m.renderMarkdown(i.notification.Body)
			m.detailView.viewport.SetContent(m.detailView.activeDetail)
		} else {
			m.detailView.activeDetail = ""
			m.detailView.viewport.SetContent("")
		}
	}
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
