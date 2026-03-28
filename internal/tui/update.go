package tui

import (
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/hirakiuc/gh-orbit/internal/api"
	"github.com/hirakiuc/gh-orbit/internal/triage"
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
		oldPage := m.listView.list.Paginator.Page
		m.listView.list, cmd = m.listView.list.Update(msg)
		cmds = append(cmds, cmd)

		// Debounced enrichment logic (after sub-model update ensures index/page is fresh)
		if m.listView.list.Index() != oldIndex || m.listView.list.Paginator.Page != oldPage {
			cmds = append(cmds, m.interpreter.Execute(ActionScheduleTick{TickType: TickEnrich}))
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) transitionGlobal(msg tea.Msg) []Action {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.handleWindowSize(msg)
	case tea.BackgroundColorMsg:
		m.handleBackgroundColor(msg)
	case notificationsLoadedMsg:
		return m.handleNotificationsLoaded(msg)
	case priorityUpdatedMsg:
		return m.handlePriorityUpdated(msg)
	case syncCompleteMsg:
		return m.handleSyncComplete(msg)
	case enrichmentBatchCompleteMsg:
		return m.handleEnrichmentBatchComplete(msg)
	case detailLoadedMsg:
		m.handleDetailLoaded(msg)
	case pollTickMsg:
		return m.handlePollTick(msg)
	case clockTickMsg:
		return m.handleClockTick(msg)
	case viewportEnrichMsg:
		if msg.ID != m.enrichID {
			return nil
		}
		return []Action{ActionEnrichItems{Notifications: m.getVisibleNotifications()}}
	case types.ErrMsg:
		m.handleTransitionError(msg)
	}

	return nil
}

func (m *Model) Transition(msg tea.Msg, oldIndex int) []Action {
	actions := m.transitionGlobal(msg)

	// State-dependent transitions
	var stateActions []Action
	switch m.state {
	case StateDetail:
		stateActions = m.transitionDetail(msg)
	case StateList:
		stateActions = m.transitionList(msg)
	}
	actions = append(actions, stateActions...)

	return actions
}

func (m *Model) transitionDetail(msg tea.Msg) []Action {
	var actions []Action
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.Matches(msg, m.keys.Back), key.Matches(msg, m.keys.ToggleDetail):
			m.state = StateList
		case key.Matches(msg, m.keys.Help):
			m.showHelp = !m.showHelp
			m.help.ShowAll = m.showHelp
		case key.Matches(msg, m.keys.OpenBrowser):
			if i, ok := m.listView.list.SelectedItem().(item); ok {
				actions = append(actions, ActionViewWeb{Notification: i.notification})
			}
		case key.Matches(msg, m.keys.CheckoutPR):
			if i, ok := m.listView.list.SelectedItem().(item); ok && i.notification.SubjectType == triage.SubjectPullRequest {
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
	m.ui.SetToast("Press 'q' again to quit")
	return nil
}

func (m *Model) transitionList(msg tea.Msg) []Action {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok || m.listView.list.FilterState() == list.Filtering {
		return nil
	}

	return m.handleListKey(keyMsg)
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

func (m *Model) getVisibleNotifications() []triage.NotificationWithState {
	start, end := m.listView.list.Paginator.GetSliceBounds(len(m.listView.list.Items()))
	if start < 0 || end > len(m.listView.list.Items()) || start >= end {
		return nil
	}
	visible := m.listView.list.Items()[start:end]

	var items []triage.NotificationWithState
	now := time.Now()

	for _, li := range visible {
		if i, ok := li.(item); ok {
			// 1. Skip if already inflight (with 30s TTL to prevent permanent blocking)
			if started, ok := m.inflightEnrichments[i.notification.GitHubID]; ok {
				if now.Sub(started) < 30*time.Second {
					continue
				}
			}

			// 2. Skip if already enriched and fresh
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
	i, ok := m.listView.list.SelectedItem().(item)
	if !ok {
		return
	}

	currentWidth := m.width - 4
	if currentWidth < 10 {
		currentWidth = 10
	}

	// 1. Determine the raw content to display
	var rawBody string
	isReady := false
	if m.ui.fetchingDetail {
		rawBody = "\n  ◌ Loading content..."
	} else if i.notification.IsEnriched {
		rawBody = i.notification.Body
		if rawBody == "" {
			rawBody = "\n  (No description provided)"
		}
		isReady = true
	} else {
		rawBody = "\n  ◌ Waiting for enrichment..."
	}

	// 2. Guard: Skip if content and width are identical to last render
	// We also check fetching state to ensure we transition from spinner to content correctly.
	if i.notification.GitHubID == m.detailView.lastRenderedID &&
		currentWidth == m.detailView.lastRenderedWidth &&
		rawBody == m.detailView.activeDetail &&
		!m.ui.fetchingDetail == isReady {
		return
	}

	// 3. Render and Wrap
	rendered := m.renderMarkdown(rawBody)
	// Apply manual wrapping using Lipgloss style
	wrapped := lipgloss.NewStyle().Width(currentWidth).Render(rendered)

	m.detailView.viewport.SetContent(wrapped)
	m.detailView.activeDetail = rawBody // Store rawBody for idempotent check
	m.detailView.lastRenderedID = i.notification.GitHubID
	m.detailView.lastRenderedWidth = currentWidth
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

		if keep && !m.isNotificationWithinVisibleAge(n, time.Now()) {
			keep = false
		}

		if keep {
			filtered = append(filtered, item{notification: n})
		}
	}

	// Preserve existing fuzzy filter state
	// Maintenance Note: This manually syncs the FilterInput sub-component.
	// Library upgrades to bubbles/v2 may change this internal structure.
	currentFilter := m.listView.list.FilterValue()

	m.listView.list.SetItems(filtered)

	// Restore fuzzy filter BEFORE restoring selection to ensure index mapping is correct
	if currentFilter != "" {
		m.listView.list.FilterInput.SetValue(currentFilter)
		m.listView.list.SetFilterText(currentFilter)
	}

	if selectedID != "" {
		for index, li := range m.listView.list.Items() {
			if i, ok := li.(item); ok && i.notification.GitHubID == selectedID {
				m.listView.list.Select(index)
				break
			}
		}
	}
}

func (m *Model) isNotificationWithinVisibleAge(n triage.NotificationWithState, now time.Time) bool {
	maxVisibleAgeDays := m.maxVisibleNotificationAgeDays()
	if maxVisibleAgeDays == 0 || n.UpdatedAt.IsZero() {
		return true
	}

	cutoff := now.AddDate(0, 0, -maxVisibleAgeDays)
	return !n.UpdatedAt.Before(cutoff)
}

func (m *Model) maxVisibleNotificationAgeDays() int {
	if m.config == nil {
		return 0
	}
	return m.config.Notifications.MaxVisibleAgeDays
}

func (m *Model) toggleResourceFilter(resType triage.SubjectType, label string) {
	if m.listView.resourceFilter == resType {
		m.listView.resourceFilter = ""
		m.ui.SetResourceFilter("")
	} else {
		m.listView.resourceFilter = resType
		m.ui.SetResourceFilter(label)
	}
	m.applyFilters()
}

func (m *Model) handleWindowSize(msg tea.WindowSizeMsg) {
	m.width = msg.Width
	m.height = msg.Height
	m.ui.SetSize(msg.Width, msg.Height)

	m.help.SetWidth(msg.Width)

	m.updateMarkdownRenderer()
}

func (m *Model) handleBackgroundColor(msg tea.BackgroundColorMsg) {
	m.isDark = msg.IsDark()
	m.styles = DefaultStyles(m.isDark)
	m.keys = NewKeyMap(m.config)
	m.listView.list.Styles.Title = m.styles.Title
	m.listView.delegate = newItemDelegate(m.styles, m.keys)
	m.listView.list.SetDelegate(m.listView.delegate)
	m.updateMarkdownRenderer()
	m.ui.SetStyles(m.styles)
}

func (m *Model) handleNotificationsLoaded(msg notificationsLoadedMsg) []Action {
	m.allNotifications = msg.notifications
	// Clear inflight on full reload to avoid stale blocks
	m.inflightEnrichments = make(map[string]time.Time)
	m.applyFilters()
	if m.state == StateDetail {
		m.refreshDetailView()
	}

	actions := []Action{
		ActionEnrichItems{Notifications: m.getVisibleNotifications()},
	}

	if msg.IsInitial && !m.syncStarted {
		m.syncStarted = true
		actions = append(actions, ActionScheduleTick{TickType: TickHeartbeat, Interval: m.heartbeatInterval})
	}

	return actions
}

func (m *Model) handlePriorityUpdated(msg priorityUpdatedMsg) []Action {
	m.allNotifications = msg.notifications
	m.applyFilters()
	return []Action{ActionShowToast{Message: msg.toast}}
}

func (m *Model) handleSyncComplete(msg syncCompleteMsg) []Action {
	m.ui.SetSyncing(false)
	m.LastSyncAt = time.Now()
	m.RateLimit = msg.rateLimit
	return []Action{
		ActionUpdateRateLimit{Info: msg.rateLimit},
		ActionLoadNotifications{IsInitial: false},
	}
}

func (m *Model) handleEnrichmentBatchComplete(msg enrichmentBatchCompleteMsg) []Action {
	m.ui.SetFetching(false)

	// Surgical update of in-memory notification slice
	for id, res := range msg.Results {
		delete(m.inflightEnrichments, id)

		for idx, n := range m.allNotifications {
			if n.GitHubID != id {
				continue
			}

			m.allNotifications[idx].ResourceState = res.ResourceState
			m.allNotifications[idx].ResourceSubState = res.ResourceSubState
			m.allNotifications[idx].IsEnriched = true
			m.allNotifications[idx].EnrichedAt.Time = res.FetchedAt
			m.allNotifications[idx].EnrichedAt.Valid = true
			break
		}
	}

	m.applyFilters()
	return nil
}

func (m *Model) handleDetailLoaded(msg detailLoadedMsg) {
	m.ui.SetFetching(false)
	delete(m.inflightEnrichments, msg.GitHubID)

	for idx, n := range m.allNotifications {
		if n.GitHubID != msg.GitHubID {
			continue
		}
		m.allNotifications[idx].Body = msg.Body
		m.allNotifications[idx].AuthorLogin = msg.Author
		m.allNotifications[idx].HTMLURL = msg.HTMLURL
		m.allNotifications[idx].ResourceState = msg.ResourceState
		m.allNotifications[idx].ResourceSubState = msg.ResourceSubState
		m.allNotifications[idx].IsEnriched = true
		break
	}

	m.applyFilters()
	if m.state == StateDetail {
		m.refreshDetailView()
	}
}

func (m *Model) handlePollTick(msg pollTickMsg) []Action {
	if msg.ID != m.heartbeatID {
		return nil
	}

	actions := []Action{ActionScheduleTick{TickType: TickHeartbeat, Interval: m.heartbeatInterval}}
	if time.Since(m.LastSyncAt).Seconds() < float64(m.PollInterval) {
		return actions
	}

	m.ui.SetSyncing(true)
	return append(actions, ActionSyncNotifications{Force: false})
}

func (m *Model) handleClockTick(msg clockTickMsg) []Action {
	if msg.ID != m.clockID {
		return nil
	}
	return []Action{ActionScheduleTick{TickType: TickClock, Interval: m.clockInterval}}
}

func (m *Model) handleTransitionError(msg types.ErrMsg) {
	m.err = msg.Err
	m.ui.SetSyncing(false)
	m.ui.SetFetching(false)
	// Clear inflight on error to allow retry
	m.inflightEnrichments = make(map[string]time.Time)
}

func (m *Model) handleListKey(msg tea.KeyMsg) []Action {
	switch {
	case msg.String() == "ctrl+c":
		return []Action{ActionQuit{}}
	case key.Matches(msg, m.keys.Quit):
		if !m.listView.list.Help.ShowAll {
			return m.handleQuitTransition()
		}
	case key.Matches(msg, m.keys.Sync):
		return m.handleSyncKey()
	case key.Matches(msg, m.keys.ToggleDetail):
		return m.handleToggleDetailKey()
	case key.Matches(msg, m.keys.ToggleRead):
		return m.handleToggleReadKey()
	case key.Matches(msg, m.keys.Help):
		m.showHelp = !m.showHelp
		m.help.ShowAll = m.showHelp
	case key.Matches(msg, m.keys.NextTab):
		m.cycleTab(1)
	case key.Matches(msg, m.keys.PrevTab):
		m.cycleTab(-1)
	case key.Matches(msg, m.keys.Tab1):
		m.setActiveTab(TabInbox)
	case key.Matches(msg, m.keys.Tab2):
		m.setActiveTab(TabUnread)
	case key.Matches(msg, m.keys.Tab3):
		m.setActiveTab(TabTriaged)
	case key.Matches(msg, m.keys.Tab4):
		m.setActiveTab(TabAll)
	case key.Matches(msg, m.keys.OpenBrowser):
		return m.handleOpenBrowserKey()
	case key.Matches(msg, m.keys.CheckoutPR):
		return m.handleCheckoutPRKey()
	case key.Matches(msg, m.keys.FilterPR):
		m.toggleResourceFilter(triage.SubjectPullRequest, "PRs")
	case key.Matches(msg, m.keys.FilterIssue):
		m.toggleResourceFilter(triage.SubjectIssue, "Issues")
	case key.Matches(msg, m.keys.FilterDiscussion):
		m.toggleResourceFilter(triage.SubjectDiscussion, "Discussions")

	case key.Matches(msg, m.keys.PriorityUp):
		return m.handlePriorityKey(1)
	case key.Matches(msg, m.keys.PriorityDown):
		return m.handlePriorityKey(-1)
	case key.Matches(msg, m.keys.PriorityNone):
		return m.handleClearPriorityKey()
	}
	return nil
}

func (m *Model) handleSyncKey() []Action {
	if m.ui.syncing {
		return nil
	}
	m.ui.SetSyncing(true)
	return []Action{ActionSyncNotifications{Force: true}}
}

func (m *Model) handleToggleDetailKey() []Action {
	n, ok := m.selectedNotification()
	if !ok {
		return nil
	}

	m.state = StateDetail
	actions := []Action{}
	if !n.IsEnriched {
		m.ui.SetFetching(true)
		actions = append(actions, ActionEnrichItems{Notifications: []triage.NotificationWithState{n}})
	}
	m.refreshDetailView()
	return actions
}

func (m *Model) handleToggleReadKey() []Action {
	n, ok := m.selectedNotification()
	if !ok {
		return nil
	}

	newState := !n.IsReadLocally
	toast := "Marked as unread"
	if newState {
		toast = "Marked as read"
	}

	return []Action{
		ActionMarkRead{ID: n.GitHubID, Read: newState},
		ActionShowToast{Message: toast},
	}
}

func (m *Model) handleOpenBrowserKey() []Action {
	n, ok := m.selectedNotification()
	if !ok {
		return nil
	}
	return []Action{ActionViewWeb{Notification: n}}
}

func (m *Model) handleCheckoutPRKey() []Action {
	n, ok := m.selectedNotification()
	if !ok || n.SubjectType != triage.SubjectPullRequest {
		return nil
	}

	number := extractNumberFromURL(n.SubjectURL)
	if number == "" {
		return nil
	}

	return []Action{ActionCheckoutPR{
		NotificationID: n.GitHubID,
		Repository:     n.RepositoryFullName,
		Number:         number,
	}}
}

func (m *Model) handlePriorityKey(delta int) []Action {
	n, ok := m.selectedNotification()
	if !ok {
		return nil
	}

	newPriority := (n.Priority + delta + 4) % 4
	return []Action{
		ActionSetPriority{ID: n.GitHubID, Priority: newPriority},
		ActionShowToast{Message: m.getPriorityToast(newPriority)},
	}
}

func (m *Model) handleClearPriorityKey() []Action {
	n, ok := m.selectedNotification()
	if !ok {
		return nil
	}
	return []Action{
		ActionSetPriority{ID: n.GitHubID, Priority: 0},
		ActionShowToast{Message: "Priority cleared"},
	}
}

func (m *Model) selectedNotification() (triage.NotificationWithState, bool) {
	i, ok := m.listView.list.SelectedItem().(item)
	if !ok {
		return triage.NotificationWithState{}, false
	}
	return i.notification, true
}

func (m *Model) cycleTab(delta int) {
	m.listView.activeTab = (m.listView.activeTab + delta + 4) % 4
	m.applyFilters()
}

func (m *Model) setActiveTab(tab int) {
	m.listView.activeTab = tab
	m.applyFilters()
}
