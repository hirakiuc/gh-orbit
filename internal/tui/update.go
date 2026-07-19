package tui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/hirakiuc/gh-orbit/internal/triage"
	"github.com/hirakiuc/gh-orbit/internal/types"
)

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Global bridge status refresh (Imperative Shell side-effect)
	m.bridgeStatus = m.backend.BridgeStatus()

	// 1. Transition State (Functional Core)
	oldIndex := m.listView.list.Index()
	actions := m.Transition(msg, oldIndex)

	// 2. Execute Actions (Imperative Shell)
	cmds := m.executeTransitionActions(actions)

	// 3. Handle sub-model updates that still use traditional TEA.
	cmds = append(cmds, m.updateUIMsg(msg)...)
	cmds = append(cmds, m.updateStateMsg(msg, oldIndex)...)

	return m, tea.Batch(cmds...)
}

func (m *Model) executeTransitionActions(actions []Action) []tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(actions))
	for _, action := range actions {
		cmds = append(cmds, m.interpreter.Execute(action))
	}
	return cmds
}

func (m *Model) updateUIMsg(msg tea.Msg) []tea.Cmd {
	// Some UI sub-models still use traditional TEA-style local updates.
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case spinner.TickMsg:
		m.ui, cmd = m.ui.Update(msg)
		return []tea.Cmd{cmd}
	case clearStatusMsg:
		m.ui, cmd = m.ui.Update(msg)
		return []tea.Cmd{cmd}
	default:
		return nil
	}
}

func (m *Model) updateStateMsg(msg tea.Msg, oldIndex int) []tea.Cmd {
	switch m.state {
	case StateDetail:
		return m.updateDetailStateMsg(msg)
	case StateList:
		return m.updateListStateMsg(msg, oldIndex)
	default:
		return nil
	}
}

func (m *Model) updateDetailStateMsg(msg tea.Msg) []tea.Cmd {
	var cmd tea.Cmd
	m.detailView.viewport, cmd = m.detailView.viewport.Update(msg)
	return []tea.Cmd{cmd}
}

func (m *Model) updateListStateMsg(msg tea.Msg, oldIndex int) []tea.Cmd {
	oldPage := m.listView.list.Paginator.Page
	oldFilter := m.listView.list.FilterValue()
	var cmd tea.Cmd
	m.listView.list, cmd = m.listView.list.Update(msg)
	cmds := []tea.Cmd{cmd}
	if m.listView.list.FilterValue() != oldFilter {
		m.clearSelection()
	}

	// Debounced enrichment logic (after sub-model update ensures index/page is fresh)
	if m.listView.list.Index() != oldIndex || m.listView.list.Paginator.Page != oldPage {
		cmds = append(cmds, m.interpreter.Execute(ActionScheduleTick{TickType: TickEnrich}))
	}

	return cmds
}

func (m *Model) transitionGlobal(msg tea.Msg) []Action {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.handleWindowSize(msg)
		return []Action{ActionScheduleTick{TickType: TickEnrich}}
	case tea.BackgroundColorMsg:
		m.handleBackgroundColor(msg)
	case notificationsLoadedMsg:
		return m.handleNotificationsLoaded(msg)
	case mutationAppliedMsg:
		return m.handleMutationApplied(msg)
	case batchMutationAppliedMsg:
		return m.handleBatchMutationApplied(msg)
	case reviewWorkspaceStartedMsg:
		return m.handleReviewWorkspaceStarted(msg)
	case syncCompleteMsg:
		return m.handleSyncComplete(msg)
	case enrichmentBatchCompleteMsg:
		return m.handleEnrichmentBatchComplete(msg)
	case detailLoadedMsg:
		m.handleDetailLoaded(msg)
	case focusModeMsg:
		m.focusMode = string(msg)
	case pollTickMsg:
		return m.handlePollTick(msg)
	case clockTickMsg:
		return m.handleClockTick(msg)
	case viewportEnrichMsg:
		if msg.ID != m.enrichID {
			return nil
		}
		return []Action{ActionEnrichItems{Notifications: m.getVisibleNotifications(false)}}
	case types.ErrMsg:
		return m.handleTransitionError(msg)
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
		case key.Matches(msg, m.keys.Sync):
			if n, ok := m.selectedNotification(); ok {
				actions = append(actions, m.handleSyncKey()...)
				actions = append(actions, ActionSetFetching{Enabled: true}, ActionFetchDetail{
					ID:          n.GitHubID,
					URL:         n.SubjectURL,
					SubjectType: n.SubjectType,
					Force:       true,
				})
			}
		case key.Matches(msg, m.keys.ToggleRead):
			return m.handleToggleReadKey()
		case key.Matches(msg, m.keys.ToggleHandled):
			return m.handleToggleHandledKey()
		case key.Matches(msg, m.keys.Help):
			m.showHelp = !m.showHelp
			m.help.ShowAll = m.showHelp
			m.syncSubModelSizes()
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
		case key.Matches(msg, m.keys.StartReviewWorkspace):
			if i, ok := m.listView.list.SelectedItem().(item); ok {
				actions = append(actions, m.handleStartReviewWorkspaceNotification(i.notification)...)
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

func (m *Model) getVisibleNotifications(force bool) []triage.NotificationWithState {
	return m.getReloadEnrichmentCandidates(force, false)
}

func (m *Model) getReloadEnrichmentCandidates(force bool, includeSelected bool) []triage.NotificationWithState {
	start, end := m.listView.list.Paginator.GetSliceBounds(len(m.listView.list.Items()))
	if start < 0 || end > len(m.listView.list.Items()) || start >= end {
		if !includeSelected {
			return nil
		}
		start, end = 0, 0
	}
	visible := m.listView.list.Items()[start:end]

	var items []triage.NotificationWithState
	seen := make(map[string]struct{})
	now := time.Now()

	statusTTL := 2 * time.Minute
	if m.config != nil {
		statusTTL = time.Duration(m.config.Enrichment.StatusTTLSeconds) * time.Second
	}

	for _, li := range visible {
		if i, ok := li.(item); ok {
			if m.isEnrichmentNeeded(i, statusTTL, force, now) {
				seen[i.notification.GitHubID] = struct{}{}
				items = append(items, i.notification)
			}
		}
	}

	if includeSelected {
		if n, ok := m.selectedNotification(); ok {
			if _, exists := seen[n.GitHubID]; !exists && m.isEnrichmentNeeded(item{notification: n}, statusTTL, force, now) {
				items = append(items, n)
			}
		}
	}

	return items
}

func (m *Model) isEnrichmentNeeded(i item, ttl time.Duration, force bool, now time.Time) bool {
	// 1. Skip if already inflight (with 30s TTL to prevent permanent blocking)
	// UNLESS we are forcing a refresh
	if !force {
		if started, ok := m.inflightEnrichments[i.notification.GitHubID]; ok {
			if now.Sub(started) < 30*time.Second {
				return false
			}
		}
	}

	// 2. Refresh if forced or not yet enriched
	if force || !i.notification.IsEnriched {
		return true
	}

	// 3. Check TTL for enriched items
	if i.notification.EnrichedAt.Valid {
		return now.Sub(i.notification.EnrichedAt.Time) > ttl
	}

	// Enriched flag set but timestamp missing? Force refresh for safety.
	return true
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

	currentFilter := m.listView.list.FilterValue()

	m.listView.list.SetItems(m.filterItems())

	m.restoreFilterState(currentFilter)
	m.restoreSelection(selectedID)
}

func (m *Model) filterItems() []list.Item {
	var filtered []list.Item
	now := time.Now()

	for _, n := range m.allNotifications {
		if m.shouldKeepNotification(n, now) {
			filtered = append(filtered, item{notification: n})
		}
	}
	return filtered
}

func (m *Model) shouldKeepNotification(n triage.NotificationWithState, now time.Time) bool {
	keep := false
	switch m.listView.activeTab {
	case TabInbox:
		keep = !n.IsHandledLocally || n.Priority > 0
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

	if keep && m.isIgnoredRepository(n.RepositoryFullName) {
		keep = false
	}

	if keep && !m.isNotificationWithinVisibleAge(n, now) {
		keep = false
	}

	return keep
}

func (m *Model) isIgnoredRepository(repoFullName string) bool {
	if m.config == nil {
		return false
	}

	repoFullName = strings.TrimSpace(repoFullName)
	if repoFullName == "" {
		return false
	}

	for _, ignored := range m.config.Notifications.IgnoreRepos {
		if strings.TrimSpace(ignored) == repoFullName {
			return true
		}
	}
	return false
}

func (m *Model) restoreFilterState(filter string) {
	if filter == "" {
		return
	}

	// Maintenance Note: This manually syncs the FilterInput sub-component.
	// Library upgrades to bubbles/v2 may change this internal structure.
	m.listView.list.FilterInput.SetValue(filter)
	m.listView.list.SetFilterText(filter)
}

func (m *Model) restoreSelection(selectedID string) {
	if selectedID == "" {
		return
	}

	for index, li := range m.listView.list.Items() {
		if i, ok := li.(item); ok && i.notification.GitHubID == selectedID {
			m.listView.list.Select(index)
			break
		}
	}
}

func (m *Model) selectNotificationByID(id string) bool {
	for index, li := range m.listView.list.Items() {
		if i, ok := li.(item); ok && i.notification.GitHubID == id {
			m.listView.list.Select(index)
			return true
		}
	}
	return false
}

func (m *Model) selectClosestIndex(previousIndex int) {
	items := m.listView.list.Items()
	if len(items) == 0 {
		return
	}
	if previousIndex < 0 {
		previousIndex = 0
	}
	if previousIndex >= len(items) {
		previousIndex = len(items) - 1
	}
	m.listView.list.Select(previousIndex)
}

func (m *Model) reconcileHandledTarget(id string, previousIndex int, optimistic bool) {
	if m.selectNotificationByID(id) {
		if m.state == StateDetail {
			m.refreshDetailView()
		}
		return
	}

	if m.state == StateDetail {
		m.state = StateList
	}
	if optimistic || id != "" {
		m.selectClosestIndex(previousIndex)
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

	m.syncSubModelSizes()
	m.updateMarkdownRenderer()
}

func (m *Model) syncSubModelSizes() {
	header := m.renderHeader()
	footer := m.renderFooter()

	m.headerHeight = lipgloss.Height(header)
	m.footerHeight = lipgloss.Height(footer)

	availHeight := m.height - m.headerHeight - m.footerHeight
	if availHeight < 0 {
		availHeight = 0
	}

	m.listView.list.SetSize(m.width, availHeight)
	m.detailView.viewport.SetWidth(m.width)
	m.detailView.viewport.SetHeight(availHeight)
}

func (m *Model) handleBackgroundColor(msg tea.BackgroundColorMsg) {
	m.isDark = msg.IsDark()
	m.styles = DefaultStyles(m.isDark)
	m.keys = NewKeyMap(m.config)
	m.listView.list.Styles.Title = m.styles.Title
	m.listView.delegate = newItemDelegate(m.styles, m.keys, m.selectedIDs)
	m.listView.list.SetDelegate(m.listView.delegate)
	m.updateMarkdownRenderer()
	m.ui.SetStyles(m.styles)
}

func (m *Model) handleNotificationsLoaded(msg notificationsLoadedMsg) []Action {
	if !m.batchPending {
		m.clearSelection()
		m.batchUncertain = false
		m.batchRefreshPending = false
	}
	m.allNotifications = msg.notifications
	// Clear inflight on full reload to avoid stale blocks
	m.inflightEnrichments = make(map[string]time.Time)
	m.applyFilters()
	if m.state == StateDetail {
		m.refreshDetailView()
	}

	candidates := m.getReloadEnrichmentCandidates(msg.IsForced, msg.IsManual)
	if msg.IsManual {
		candidates = m.filterSelectedDetailRefreshCandidate(candidates)
	}

	actions := []Action{
		ActionEnrichItems{
			Notifications: candidates,
			Force:         msg.IsForced,
		},
	}

	if msg.IsManual {
		toast := "Sync complete"
		if notificationStateSignature(msg.notifications) == m.manualSyncSnapshot {
			toast = "No new notifications"
		}
		m.manualSyncPending = false
		m.manualSyncSnapshot = ""
		actions = append(actions, ActionShowToast{Message: toast})
	}

	if msg.IsInitial && !m.syncStarted {
		m.syncStarted = true
		actions = append(actions, ActionScheduleTick{TickType: TickHeartbeat, Interval: m.heartbeatInterval})
	}

	return actions
}

func (m *Model) filterSelectedDetailRefreshCandidate(notifications []triage.NotificationWithState) []triage.NotificationWithState {
	if m.state != StateDetail {
		return notifications
	}

	selected, ok := m.selectedNotification()
	if !ok {
		return notifications
	}

	filtered := notifications[:0]
	for _, n := range notifications {
		if n.GitHubID != selected.GitHubID {
			filtered = append(filtered, n)
		}
	}
	return filtered
}

func (m *Model) handleMutationApplied(msg mutationAppliedMsg) []Action {
	m.allNotifications = msg.notifications
	m.applyFilters()
	if msg.reconcileItem {
		m.reconcileHandledTarget(msg.targetID, msg.previousIndex, false)
	}
	m.err = msg.err
	if msg.toast == "" {
		return nil
	}
	return []Action{ActionShowToast{Message: msg.toast}}
}

func (m *Model) handleBatchMutationApplied(msg batchMutationAppliedMsg) []Action {
	m.batchPending = false
	m.pendingBatchRequest = types.NotificationBatchRequest{}
	m.err = msg.result.Err
	m.batchUncertain = msg.result.Status == types.NotificationBatchCommitUnknown
	m.batchRefreshPending = msg.result.Reconciliation == types.NotificationBatchReconciliationPending

	switch msg.result.Status {
	case types.NotificationBatchRejected:
		m.allNotifications = msg.before
	case types.NotificationBatchCommitted:
		m.allNotifications = msg.result.Notifications
		clear(m.selectedIDs)
		for _, outcome := range msg.result.Outcomes {
			switch outcome.Status {
			case types.NotificationRemoteFailed, types.NotificationRemoteCanceled, types.NotificationRemoteNotAttempted:
				m.selectedIDs[outcome.ID] = struct{}{}
			}
		}
		m.selectionMode = len(m.selectedIDs) > 0
	case types.NotificationBatchCommitUnknown:
		if msg.result.Notifications != nil {
			m.allNotifications = msg.result.Notifications
		}
		m.selectionMode = true
		clear(m.selectedIDs)
		for _, id := range msg.result.Request.IDs {
			m.selectedIDs[id] = struct{}{}
		}
	}
	m.applyFilters()

	toast := msg.result.Toast
	if toast == "" {
		switch msg.result.Status {
		case types.NotificationBatchRejected:
			toast = "Batch update failed"
		case types.NotificationBatchCommitUnknown:
			toast = "Batch outcome unknown; refreshing"
		default:
			if m.batchRefreshPending {
				toast = "Batch committed; refresh pending"
			} else if len(m.selectedIDs) > 0 {
				toast = fmt.Sprintf("Batch committed; %d remote updates need retry", len(m.selectedIDs))
			} else {
				toast = "Batch update complete"
			}
		}
	}
	actions := []Action{ActionShowToast{Message: toast}}
	if m.batchUncertain || m.batchRefreshPending {
		actions = append(actions, ActionLoadNotifications{})
	}
	return actions
}

func (m *Model) handleReviewWorkspaceStarted(msg reviewWorkspaceStartedMsg) []Action {
	if msg.toast == "" {
		return nil
	}
	return []Action{ActionShowToast{Message: msg.toast}}
}

func (m *Model) handleSyncComplete(msg syncCompleteMsg) []Action {
	m.ui.SetSyncing(false)
	m.LastSyncAt = time.Now()
	m.RateLimit = msg.rateLimit
	m.updateQuotaResetStatus()
	return []Action{
		ActionUpdateRateLimit{Info: msg.rateLimit},
		ActionLoadNotifications{IsInitial: false, IsForced: msg.IsForced, IsManual: msg.IsManual},
	}
}

func (m *Model) handleEnrichmentBatchComplete(msg enrichmentBatchCompleteMsg) []Action {
	m.ui.SetFetching(false)

	if len(msg.Results) > 0 {
		m.logger.Debug("tui: enrichment batch complete", "result_count", len(msg.Results))
	}

	// Surgical update of in-memory notification slice
	for nodeID, res := range msg.Results {
		for idx, n := range m.allNotifications {
			if n.SubjectNodeID != nodeID {
				continue
			}

			// Clear inflight for each notification matching this nodeID
			delete(m.inflightEnrichments, n.GitHubID)

			// Log state transition if it changed
			if m.allNotifications[idx].ResourceState != res.ResourceState {
				m.logger.Info("tui: item state changed",
					"id", n.GitHubID,
					"old_state", m.allNotifications[idx].ResourceState,
					"new_state", res.ResourceState)
			}

			m.allNotifications[idx].ResourceState = res.ResourceState
			m.allNotifications[idx].ResourceSubState = res.ResourceSubState
			m.allNotifications[idx].IsEnriched = true
			m.allNotifications[idx].EnrichedAt.Time = res.FetchedAt
			m.allNotifications[idx].EnrichedAt.Valid = true
		}
	}

	m.applyFilters()
	return nil
}

func (m *Model) handleDetailLoaded(msg detailLoadedMsg) {
	m.ui.SetFetching(false)
	delete(m.inflightEnrichments, msg.GitHubID)

	m.logger.Debug("tui: detail loaded", "id", msg.GitHubID, "state", msg.ResourceState)

	for idx, n := range m.allNotifications {
		if n.GitHubID != msg.GitHubID {
			continue
		}

		// Log state transition if it changed
		if m.allNotifications[idx].ResourceState != msg.ResourceState {
			m.logger.Info("tui: item state changed",
				"id", n.GitHubID,
				"old_state", m.allNotifications[idx].ResourceState,
				"new_state", msg.ResourceState)
		}

		m.allNotifications[idx].SubjectNodeID = msg.SubjectNodeID
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

	actions := []Action{
		ActionScheduleTick{TickType: TickHeartbeat, Interval: m.heartbeatInterval},
		ActionScheduleTick{TickType: TickEnrich},
	}
	if time.Since(m.LastSyncAt).Seconds() < float64(m.PollInterval) {
		return actions
	}

	return append(
		actions,
		ActionSetSyncing{Enabled: true},
		ActionSyncNotifications{Force: false, IsManual: false},
	)
}

func (m *Model) handleClockTick(msg clockTickMsg) []Action {
	if msg.ID != m.clockID {
		return nil
	}
	m.updateQuotaResetStatus()
	return []Action{
		ActionScheduleTick{TickType: TickClock, Interval: m.clockInterval},
		ActionCheckFocusMode{},
	}
}

func (m *Model) handleTransitionError(msg types.ErrMsg) []Action {
	m.err = msg.Err
	m.ui.SetSyncing(false)
	m.ui.SetFetching(false)
	// Clear inflight on error to allow retry
	m.inflightEnrichments = make(map[string]time.Time)

	if m.manualSyncPending {
		m.manualSyncPending = false
		m.manualSyncSnapshot = ""
		return []Action{ActionShowToast{Message: "Sync failed"}}
	}

	if errors.Is(msg.Err, types.ErrReviewWorkspaceUnsupported) {
		return []Action{ActionShowToast{Message: "Review workspace start is unavailable in this session"}}
	}

	return nil
}

func (m *Model) handleListKey(msg tea.KeyMsg) []Action {
	if msg.String() == "ctrl+c" {
		return []Action{ActionQuit{}}
	}
	if actions, handled := m.handleSelectionKeys(msg); handled {
		return actions
	}

	if actions := m.handleNavigationKeys(msg); actions != nil {
		return actions
	}

	if actions := m.handleTabKeys(msg); actions != nil {
		return actions
	}

	if actions := m.handleFilteringKeys(msg); actions != nil {
		return actions
	}

	return m.handlePriorityKeys(msg)
}

func (m *Model) clearSelection() {
	m.selectionMode = false
	clear(m.selectedIDs)
}

func (m *Model) handleSelectionKeys(msg tea.KeyMsg) ([]Action, bool) {
	if key.Matches(msg, m.keys.SelectionMode) {
		if m.batchPending {
			return []Action{ActionShowToast{Message: "Batch update already in progress"}}, true
		}
		if m.selectionMode {
			m.clearSelection()
		} else {
			m.selectionMode = true
		}
		return []Action{}, true
	}
	if !m.selectionMode {
		return nil, false
	}
	if m.batchPending {
		return []Action{ActionShowToast{Message: "Batch update already in progress"}}, true
	}

	switch {
	case key.Matches(msg, m.keys.Back):
		m.clearSelection()
		return []Action{}, true
	case key.Matches(msg, m.keys.SelectNotification):
		if notification, ok := m.selectedNotification(); ok {
			if _, selected := m.selectedIDs[notification.GitHubID]; selected {
				delete(m.selectedIDs, notification.GitHubID)
			} else {
				m.selectedIDs[notification.GitHubID] = struct{}{}
			}
		}
		return []Action{}, true
	case key.Matches(msg, m.keys.BatchRead):
		return m.notificationBatchAction(types.NotificationBatchRead), true
	case key.Matches(msg, m.keys.BatchUnread):
		return m.notificationBatchAction(types.NotificationBatchUnread), true
	case key.Matches(msg, m.keys.BatchHandled):
		return m.notificationBatchAction(types.NotificationBatchHandled), true
	case key.Matches(msg, m.keys.BatchUnhandled):
		return m.notificationBatchAction(types.NotificationBatchUnhandled), true
	case key.Matches(msg, m.keys.NextTab), key.Matches(msg, m.keys.PrevTab),
		key.Matches(msg, m.keys.Tab1), key.Matches(msg, m.keys.Tab2), key.Matches(msg, m.keys.Tab3),
		key.Matches(msg, m.keys.FilterPR), key.Matches(msg, m.keys.FilterIssue), key.Matches(msg, m.keys.FilterDiscussion),
		key.Matches(msg, m.keys.ToggleDetail):
		m.clearSelection()
		return nil, false
	default:
		// Leave unbound navigation to the Bubbles list while suppressing
		// scalar actions during selection mode.
		return nil, true
	}
}

func (m *Model) notificationBatchAction(operation types.NotificationBatchOperation) []Action {
	request, ok := m.selectedBatchRequest(operation)
	if !ok {
		return []Action{ActionShowToast{Message: "Select at least one notification"}}
	}
	return []Action{ActionApplyNotificationBatch{Request: request}}
}

func (m *Model) handleNavigationKeys(msg tea.KeyMsg) []Action {
	switch {
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
	case key.Matches(msg, m.keys.ToggleHandled):
		return m.handleToggleHandledKey()
	case key.Matches(msg, m.keys.Help):
		m.showHelp = !m.showHelp
		m.help.ShowAll = m.showHelp
		m.syncSubModelSizes()
	case key.Matches(msg, m.keys.OpenBrowser):
		return m.handleOpenBrowserKey()
	case key.Matches(msg, m.keys.CheckoutPR):
		return m.handleCheckoutPRKey()
	case key.Matches(msg, m.keys.StartReviewWorkspace):
		return m.handleStartReviewWorkspaceKey()
	}
	return nil
}

func (m *Model) handleTabKeys(msg tea.KeyMsg) []Action {
	switch {
	case key.Matches(msg, m.keys.NextTab):
		m.cycleTab(1)
	case key.Matches(msg, m.keys.PrevTab):
		m.cycleTab(-1)
	case key.Matches(msg, m.keys.Tab1):
		m.setActiveTab(TabInbox)
	case key.Matches(msg, m.keys.Tab2):
		m.setActiveTab(TabTriaged)
	case key.Matches(msg, m.keys.Tab3):
		m.setActiveTab(TabAll)
	default:
		return nil
	}
	return []Action{} // State changed, but no imperative actions
}

func (m *Model) handleFilteringKeys(msg tea.KeyMsg) []Action {
	switch {
	case key.Matches(msg, m.keys.FilterPR):
		m.toggleResourceFilter(triage.SubjectPullRequest, "PRs")
	case key.Matches(msg, m.keys.FilterIssue):
		m.toggleResourceFilter(triage.SubjectIssue, "Issues")
	case key.Matches(msg, m.keys.FilterDiscussion):
		m.toggleResourceFilter(triage.SubjectDiscussion, "Discussions")
	default:
		return nil
	}
	return []Action{}
}

func (m *Model) handlePriorityKeys(msg tea.KeyMsg) []Action {
	switch {
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
		return []Action{ActionShowToast{Message: "Sync already in progress"}}
	}
	m.manualSyncPending = true
	m.manualSyncSnapshot = notificationStateSignature(m.allNotifications)
	return []Action{
		ActionSetSyncing{Enabled: true},
		ActionSyncNotifications{Force: true, IsManual: true},
	}
}

func (m *Model) handleToggleDetailKey() []Action {
	n, ok := m.selectedNotification()
	if !ok {
		return nil
	}

	m.state = StateDetail
	actions := []Action{}
	if !n.IsEnriched {
		actions = append(
			actions,
			ActionSetFetching{Enabled: true},
			ActionEnrichItems{Notifications: []triage.NotificationWithState{n}},
		)
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

func (m *Model) handleToggleHandledKey() []Action {
	n, ok := m.selectedNotification()
	if !ok {
		return nil
	}

	newState := !n.IsHandledLocally
	toast := "Marked as unhandled"
	if newState {
		toast = "Marked as handled"
	}

	return []Action{
		ActionSetHandled{ID: n.GitHubID, Handled: newState, PreviousIndex: m.listView.list.Index()},
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

func (m *Model) handleStartReviewWorkspaceKey() []Action {
	n, ok := m.selectedNotification()
	if !ok {
		return nil
	}
	return m.handleStartReviewWorkspaceNotification(n)
}

func (m *Model) handleStartReviewWorkspaceNotification(n triage.NotificationWithState) []Action {
	if n.SubjectType != triage.SubjectPullRequest {
		return nil
	}

	number := extractNumberFromURL(n.SubjectURL)
	if number == "" {
		return nil
	}

	repository, ok := extractReviewWorkspaceRepository(n)
	if !ok {
		return nil
	}

	pullRequestNumber, err := strconv.Atoi(number)
	if err != nil || pullRequestNumber <= 0 {
		return nil
	}

	return []Action{ActionStartReviewWorkspace{
		NotificationID:    n.GitHubID,
		Repository:        repository,
		PullRequestNumber: pullRequestNumber,
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
	m.listView.activeTab = (m.listView.activeTab + delta + tabCount) % tabCount
	m.applyFilters()
}

func (m *Model) setActiveTab(tab int) {
	m.listView.activeTab = tab
	m.applyFilters()
}
