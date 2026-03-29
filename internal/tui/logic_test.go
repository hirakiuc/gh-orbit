package tui

import (
	"database/sql"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/mocks"
	"github.com/hirakiuc/gh-orbit/internal/models"
	"github.com/hirakiuc/gh-orbit/internal/triage"
	"github.com/hirakiuc/gh-orbit/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// keyPress is a helper to create a KeyPressMsg for tests.
func keyPress(s string) tea.KeyPressMsg {
	switch s {
	case " ":
		return tea.KeyPressMsg{Code: ' '}
	case "space":
		return tea.KeyPressMsg{Code: ' '}
	case "enter":
		return tea.KeyPressMsg{Text: "enter"}
	case "tab":
		return tea.KeyPressMsg{Text: "tab"}
	case "esc":
		return tea.KeyPressMsg{Text: "esc"}
	case "shift+tab":
		return tea.KeyPressMsg{Text: "shift+tab"}
	case "shift+up":
		return tea.KeyPressMsg{Text: "shift+up"}
	case "down":
		// bubbles/v2 list often uses 'j' or KeyDown
		return tea.KeyPressMsg{Code: 'j'}
	default:
		return tea.KeyPressMsg{Text: s}
	}
}

func TestInterpreter_Execute(t *testing.T) {
	m := newTestModel(t)
	interp := NewInterpreter(m)

	// Mock Submit for all actions that use it
	m.traffic.(*mocks.MockTrafficController).EXPECT().Submit(mock.Anything, mock.Anything).Return(func() tea.Msg { return nil }).Maybe()
	m.traffic.(*mocks.MockTrafficController).EXPECT().UpdateRateLimit(mock.Anything, mock.Anything).Return().Maybe()

	mockExecutor := m.executor.(*mocks.MockCommandExecutor)
	mockExecutor.EXPECT().InteractiveGH(mock.Anything, "pr", "checkout", "1", "-R", "o/r").Return(func() tea.Msg { return nil }).Maybe()
	mockExecutor.EXPECT().Run(mock.Anything, "gh", "pr", "view", "1", "-R", "o/r", "--web").Return(nil).Maybe()

	notif := triage.NotificationWithState{
		Notification: triage.Notification{
			GitHubID:           "1",
			SubjectURL:         "https://api.github.com/repos/o/r/pulls/1",
			SubjectType:        "PullRequest",
			RepositoryFullName: "o/r",
		},
	}

	actions := []Action{
		ActionQuit{},
		ActionShowToast{Message: "msg"},
		ActionSyncNotifications{Force: true},
		ActionMarkRead{ID: "1", Read: true},
		ActionSetPriority{ID: "1", Priority: 1},
		ActionViewWeb{Notification: notif},
		ActionCheckoutPR{Repository: "o/r", Number: "1"},
		ActionEnrichItems{Notifications: []triage.NotificationWithState{notif}},
		ActionLoadNotifications{IsInitial: true},
		ActionUpdateRateLimit{Info: models.RateLimitInfo{Remaining: 100}},
		ActionScheduleTick{TickType: TickHeartbeat, Interval: time.Millisecond},
		ActionScheduleTick{TickType: TickClock, Interval: time.Millisecond},
		ActionScheduleTick{TickType: TickToast, Interval: time.Millisecond},
	}

	for _, a := range actions {
		cmd := interp.Execute(a)
		assert.NotNil(t, cmd)
		if a.Type() == "show_toast" {
			_ = executeCmd(cmd)
			assert.Equal(t, "msg", m.ui.toastMessage)
		}
	}
}

func TestModel_Transition_EdgeCases(t *testing.T) {
	m := newTestModel(t)
	m.db.(*mocks.MockRepository).EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()
	m.allNotifications = []triage.NotificationWithState{{Notification: triage.Notification{GitHubID: "1"}}}
	m.applyFilters()

	// 1. Detail Loaded (No actions returned, just state update)
	actions := m.Transition(detailLoadedMsg{GitHubID: "1", Body: "details"}, 0)
	assert.Equal(t, 0, len(actions))

	// 2. Priority Updated
	actions = m.Transition(priorityUpdatedMsg{toast: "updated"}, 0)
	assert.Contains(t, actions, ActionShowToast{Message: "updated"})

	// 3. Sync Complete
	actions = m.Transition(syncCompleteMsg{rateLimit: models.RateLimitInfo{Remaining: 500}}, 0)
	assert.Contains(t, actions, ActionLoadNotifications{IsInitial: false})
	assert.Equal(t, 500, m.RateLimit.Remaining)
}

func TestModel_Transition_Navigation(t *testing.T) {
	m := newTestModel(t)
	m.db.(*mocks.MockRepository).EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()

	notif := triage.NotificationWithState{
		Notification: triage.Notification{
			GitHubID:    "1",
			SubjectURL:  "https://api.github.com/repos/o/r/pulls/1",
			SubjectType: "PullRequest",
			UpdatedAt:   time.Now(),
		},
	}
	m.allNotifications = []triage.NotificationWithState{notif}
	m.applyFilters()
	m.listView.list.Select(0)

	// Initial State: List
	assert.Equal(t, StateList, m.state)

	// 1. Enter Detail View
	msg := keyPress("space") // Use space word, it matches "space" binding
	actions := m.Transition(msg, 0)
	assert.Equal(t, StateDetail, m.state)
	// Should return ActionEnrichItems because notif.IsEnriched is false
	assert.Contains(t, actions, ActionEnrichItems{Notifications: []triage.NotificationWithState{notif}})

	// 2. Return to List View
	msg = keyPress("esc")
	_ = m.Transition(msg, 0)
	assert.Equal(t, StateList, m.state)
}

func TestModel_Transition_Priority(t *testing.T) {
	m := newTestModel(t)
	m.db.(*mocks.MockRepository).EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()

	notif := triage.NotificationWithState{
		Notification: triage.Notification{GitHubID: "1"},
		State:        triage.State{Priority: 0},
	}
	m.allNotifications = []triage.NotificationWithState{notif}
	m.applyFilters()
	m.listView.list.Select(0)

	// 1. Set Priority via key binding
	msg := keyPress("shift+up")
	actions := m.Transition(msg, 0)
	assert.Contains(t, actions, ActionSetPriority{ID: "1", Priority: 1})

	// 2. Clear Priority
	msg = keyPress("0") // Matches PriorityNone
	actions = m.Transition(msg, 0)
	assert.Contains(t, actions, ActionSetPriority{ID: "1", Priority: 0})
}

func TestModel_Transition_Tabs(t *testing.T) {
	m := newTestModel(t)
	m.db.(*mocks.MockRepository).EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()

	// 1. Next Tab
	msg := keyPress("tab")
	initialTab := m.listView.activeTab
	_ = m.Transition(msg, 0)
	assert.Equal(t, (initialTab+1)%4, m.listView.activeTab)

	// 2. Prev Tab
	msg = keyPress("shift+tab")
	_ = m.Transition(msg, 0)
	assert.Equal(t, initialTab, m.listView.activeTab)
}

func TestModel_Transition_Enrichment(t *testing.T) {
	m := newTestModel(t)
	m.db.(*mocks.MockRepository).EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()

	notifs := []triage.NotificationWithState{
		{Notification: triage.Notification{GitHubID: "1", IsEnriched: false, UpdatedAt: time.Now()}},
		{Notification: triage.Notification{GitHubID: "2", IsEnriched: false, UpdatedAt: time.Now()}},
	}
	m.allNotifications = notifs
	m.applyFilters()

	// Ensure list size is enough to show both items
	m.listView.list.SetSize(100, 100)
	m.listView.list.Select(0)
	oldIndex := 0

	// 1. Viewport enrichment msg (ID check)
	m.enrichID = 42
	actions := m.Transition(viewportEnrichMsg{ID: 42}, oldIndex)
	assert.Contains(t, actions, ActionEnrichItems{Notifications: []triage.NotificationWithState{notifs[0], notifs[1]}})

	// 2. List index change (debounced enrichment)
	// oldIndex is 0
	_, cmd := m.Update(keyPress("down"))
	assert.NotNil(t, cmd, "Update should return commands for enrichment tick")
}

func TestModel_Enrichment_Deduplication(t *testing.T) {
	m := newTestModel(t)
	m.db.(*mocks.MockRepository).EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()

	notif := triage.NotificationWithState{
		Notification: triage.Notification{GitHubID: "1", IsEnriched: false, UpdatedAt: time.Now()},
	}
	m.allNotifications = []triage.NotificationWithState{notif}
	m.applyFilters()
	m.listView.list.SetSize(100, 100)

	// 1. Mark as inflight
	m.inflightEnrichments["1"] = time.Now()

	// 2. Trigger enrichment
	actions := m.Transition(viewportEnrichMsg{ID: m.enrichID}, 0)
	// Should NOT contain ActionEnrichItems because it's already inflight
	for _, a := range actions {
		if ae, ok := a.(ActionEnrichItems); ok {
			assert.Empty(t, ae.Notifications, "Should not re-enrich inflight item")
		}
	}
}

func TestModel_Enrichment_SurgicalUpdate(t *testing.T) {
	m := newTestModel(t)
	m.db.(*mocks.MockRepository).EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()

	notif := triage.NotificationWithState{
		Notification: triage.Notification{
			GitHubID:      "1",
			SubjectNodeID: "node_1",
			IsEnriched:    false,
			UpdatedAt:     time.Now(),
		},
	}
	m.allNotifications = []triage.NotificationWithState{notif}
	m.inflightEnrichments["1"] = time.Now()

	// 1. Receive surgical update (indexed by SubjectNodeID)
	results := map[string]models.EnrichmentResult{
		"node_1": {ResourceState: "Merged", ResourceSubState: "APPROVED", FetchedAt: time.Now()},
	}
	_ = m.Transition(enrichmentBatchCompleteMsg{Results: results}, 0)

	// 2. Assertions
	assert.True(t, m.allNotifications[0].IsEnriched)
	assert.Equal(t, "Merged", m.allNotifications[0].ResourceState)
	assert.Empty(t, m.inflightEnrichments, "Inflight map should be cleared")
}

func TestModel_Transition_Filtering(t *testing.T) {
	m := newTestModel(t)
	m.db.(*mocks.MockRepository).EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()

	// 1. Filter PRs
	msg := keyPress("p") // matches FilterPR
	_ = m.Transition(msg, 0)
	assert.Equal(t, triage.SubjectPullRequest, m.listView.resourceFilter)

	// 2. Toggle off
	_ = m.Transition(msg, 0)
	assert.Equal(t, triage.SubjectType(""), m.listView.resourceFilter)

	// 3. Filter Discussions
	msg = keyPress("d") // matches FilterDiscussion
	_ = m.Transition(msg, 0)
	assert.Equal(t, triage.SubjectDiscussion, m.listView.resourceFilter)
}

func TestModel_ApplyFilters_HidesNotificationsOlderThanConfiguredDays(t *testing.T) {
	m := newTestModel(t)
	m.listView.activeTab = TabAll
	m.config.Notifications.MaxVisibleAgeDays = 365

	oldNotification := triage.NotificationWithState{
		Notification: triage.Notification{
			GitHubID:    "old",
			SubjectType: "PullRequest",
			UpdatedAt:   daysAgo(366),
		},
	}
	recentNotification := triage.NotificationWithState{
		Notification: triage.Notification{
			GitHubID:    "recent",
			SubjectType: "PullRequest",
			UpdatedAt:   daysAgo(30),
		},
	}

	m.allNotifications = []triage.NotificationWithState{oldNotification, recentNotification}
	m.applyFilters()

	require.Len(t, m.listView.list.Items(), 1)
	item, ok := m.listView.list.Items()[0].(item)
	require.True(t, ok)
	assert.Equal(t, "recent", item.notification.GitHubID)
}

func TestModel_ApplyFilters_ZeroVisibleAgeDaysDisablesCutoff(t *testing.T) {
	m := newTestModel(t)
	m.listView.activeTab = TabAll
	m.config.Notifications.MaxVisibleAgeDays = 0

	m.allNotifications = []triage.NotificationWithState{
		{Notification: triage.Notification{GitHubID: "old", SubjectType: "PullRequest", UpdatedAt: daysAgo(900)}},
		{Notification: triage.Notification{GitHubID: "recent", SubjectType: "PullRequest", UpdatedAt: daysAgo(30)}},
	}
	m.applyFilters()

	assert.Len(t, m.listView.list.Items(), 2)
}

func TestModel_ApplyFilters_UsesGitHubUpdatedAtInsteadOfRecentLocalActivity(t *testing.T) {
	m := newTestModel(t)
	m.listView.activeTab = TabAll
	m.config.Notifications.MaxVisibleAgeDays = 365

	oldButRecentlyTouched := triage.NotificationWithState{
		Notification: triage.Notification{
			GitHubID:      "old",
			SubjectType:   "PullRequest",
			UpdatedAt:     daysAgo(500),
			IsEnriched:    true,
			EnrichedAt:    sql.NullTime{Time: time.Now(), Valid: true},
			ResourceState: "OPEN",
		},
		State: triage.State{
			Priority:      3,
			IsReadLocally: true,
		},
	}
	recentNotification := triage.NotificationWithState{
		Notification: triage.Notification{
			GitHubID:    "recent",
			SubjectType: "PullRequest",
			UpdatedAt:   daysAgo(10),
		},
	}

	m.allNotifications = []triage.NotificationWithState{oldButRecentlyTouched, recentNotification}
	m.applyFilters()

	require.Len(t, m.listView.list.Items(), 1)
	item, ok := m.listView.list.Items()[0].(item)
	require.True(t, ok)
	assert.Equal(t, "recent", item.notification.GitHubID)
}

func TestHelpers(t *testing.T) {
	// extractNumberFromURL
	assert.Equal(t, "123", extractNumberFromURL("https://api.github.com/repos/o/r/pulls/123"))
	assert.Equal(t, "", extractNumberFromURL("invalid"))

	// extractTagFromURL
	assert.Equal(t, "v1.0", extractTagFromURL("https://api.github.com/repos/o/r/releases/v1.0"))
	assert.Equal(t, "", extractTagFromURL("invalid tag"))

	// isValidGitHubURL
	assert.True(t, isValidGitHubURL("https://github.com/o/r"))
	assert.False(t, isValidGitHubURL("https://google.com"))
}

func TestModel_Transition_Global(t *testing.T) {
	m := newTestModel(t)
	m.db.(*mocks.MockRepository).EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()

	// Window Size
	actions := m.Transition(tea.WindowSizeMsg{Width: 100, Height: 50}, 0)
	assert.Equal(t, 100, m.width)
	assert.Equal(t, 50, m.height)
	assert.Contains(t, actions, ActionScheduleTick{TickType: TickEnrich})

	// Error Msg
	msg := types.ErrMsg{Err: assert.AnError}
	_ = m.Transition(msg, 0)
	assert.Equal(t, assert.AnError, m.err)
}
