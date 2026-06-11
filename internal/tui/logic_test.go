package tui

import (
	"context"
	"database/sql"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/api"
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

func assertListNotificationIDs(t *testing.T, m *Model, expected []string) {
	t.Helper()

	items := m.listView.list.Items()
	require.Len(t, items, len(expected))
	for idx, expectedID := range expected {
		item, ok := items[idx].(item)
		require.True(t, ok)
		assert.Equal(t, expectedID, item.notification.GitHubID)
	}
}

func TestInterpreter_Execute(t *testing.T) {
	m := newTestModel(t)
	interp := NewInterpreter(m)

	// Mock Submit for all actions that use it
	m.traffic.(*mocks.MockTrafficController).EXPECT().Submit(mock.Anything, mock.Anything, mock.Anything).Return(make(chan any), nil).Maybe()
	m.traffic.(*mocks.MockTrafficController).EXPECT().UpdateRateLimit(mock.Anything, mock.Anything).Return().Maybe()

	mockExecutor := m.executor.(*mocks.MockCommandExecutor)
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
		ActionSetSyncing{Enabled: true},
		ActionSetFetching{Enabled: true},
		ActionSyncNotifications{Force: true, IsManual: false},
		ActionMarkRead{ID: "1", Read: true},
		ActionSetPriority{ID: "1", Priority: 1},
		ActionViewWeb{Notification: notif},
		ActionCheckoutPR{Repository: "o/r", Number: "1"},
		ActionEnrichItems{Notifications: []triage.NotificationWithState{notif}},
		ActionLoadNotifications{IsInitial: true, IsManual: false},
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

func TestInterpreter_Execute_UpdateRateLimitConnectedMode(t *testing.T) {
	m := newTestModel(t)
	m.traffic = nil
	interp := NewInterpreter(m)

	cmd := interp.Execute(ActionUpdateRateLimit{Info: models.RateLimitInfo{Remaining: 321}})
	require.NotNil(t, cmd)

	msg := executeCmd(cmd)
	assert.Nil(t, msg)
	assert.Equal(t, 321, m.RateLimit.Remaining)
}

func TestInterpreter_Execute_UpdateRateLimitStandaloneMode(t *testing.T) {
	m := newTestModel(t)
	traffic := m.traffic.(*mocks.MockTrafficController)
	traffic.EXPECT().UpdateRateLimit(mock.Anything, models.RateLimitInfo{Remaining: 123}).Return().Once()
	interp := NewInterpreter(m)

	cmd := interp.Execute(ActionUpdateRateLimit{Info: models.RateLimitInfo{Remaining: 123}})
	require.NotNil(t, cmd)

	msg := executeCmd(cmd)
	assert.Nil(t, msg)
	assert.Equal(t, 123, m.RateLimit.Remaining)
}

func TestModel_submitTask_SubmitErrorReturnsErrMsg(t *testing.T) {
	m := newTestModel(t)
	m.traffic.(*mocks.MockTrafficController).EXPECT().
		Submit(mock.Anything, api.PrioritySync, mock.Anything).
		Return(nil, api.ErrTrafficQueueFull).
		Once()

	cmd := m.submitTask("sync:test", 0, api.PrioritySync, func(ctx context.Context) any { return "unreachable" })
	require.NotNil(t, cmd)

	msg := executeCmd(cmd)
	errMsg, ok := msg.(types.ErrMsg)
	require.True(t, ok)
	assert.ErrorIs(t, errMsg.Err, api.ErrTrafficQueueFull)
}

func TestModel_SyncNotificationsWithForce_ConnectedModeTimeoutClearsSyncing(t *testing.T) {
	m := newTestModel(t)
	m.traffic = nil
	m.ui.SetSyncing(true)

	originalTimeout := types.ConnectedSyncTimeout
	types.ConnectedSyncTimeout = 10 * time.Millisecond
	t.Cleanup(func() {
		types.ConnectedSyncTimeout = originalTimeout
	})

	testSyncer(m).EXPECT().
		Sync(mock.Anything, "test-user", true).
		RunAndReturn(func(ctx context.Context, userID string, force bool) (models.RateLimitInfo, error) {
			<-ctx.Done()
			return models.RateLimitInfo{}, ctx.Err()
		}).
		Once()

	cmd := m.syncNotificationsWithForce(true, false)
	require.NotNil(t, cmd)

	msg := executeCmd(cmd)
	errMsg, ok := msg.(types.ErrMsg)
	require.True(t, ok)
	assert.ErrorIs(t, errMsg.Err, context.DeadlineExceeded)

	_ = m.handleTransitionError(errMsg)
	assert.False(t, m.ui.syncing)
	assert.ErrorIs(t, m.err, context.DeadlineExceeded)
}

func TestModel_submitTaskScopedRequestCancelsPreviousTask(t *testing.T) {
	m := newTestModel(t)
	m.traffic = nil

	started := make(chan struct{})
	firstDone := make(chan tea.Msg, 1)
	first := m.submitTask("shared-scope", 0, api.PrioritySync, func(ctx context.Context) any {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})

	go func() {
		firstDone <- executeCmd(first)
	}()

	<-started
	second := m.submitTask("shared-scope", 0, api.PrioritySync, func(ctx context.Context) any {
		return "second"
	})
	require.NotNil(t, second)
	assert.Equal(t, "second", executeCmd(second))
	assert.ErrorIs(t, (<-firstDone).(error), context.Canceled)
}

func TestModel_SyncNotificationsWithForce_ConnectedModeTreatsIntervalNotReachedAsBenign(t *testing.T) {
	m := newTestModel(t)
	m.traffic = nil
	m.ui.SetSyncing(true)

	testSyncer(m).EXPECT().
		Sync(mock.Anything, "test-user", false).
		Return(models.RateLimitInfo{Remaining: 999}, types.ErrSyncIntervalNotReached).
		Once()

	cmd := m.syncNotificationsWithForce(false, false)
	require.NotNil(t, cmd)

	msg := executeCmd(cmd)
	syncMsg, ok := msg.(syncCompleteMsg)
	require.True(t, ok)
	assert.Equal(t, models.RateLimitInfo{Remaining: 999}, syncMsg.rateLimit)
	assert.False(t, syncMsg.IsForced)

	actions := m.handleSyncComplete(syncMsg)
	assert.False(t, m.ui.syncing)
	assert.Contains(t, actions, ActionLoadNotifications{IsInitial: false, IsForced: false, IsManual: false})
}

func TestModel_MarkReadByID_ConnectedModeDoesNotRequireClient(t *testing.T) {
	m := newTestModel(t)
	testBackend(m).Client = nil
	m.traffic = nil
	m.allNotifications = []triage.NotificationWithState{
		{Notification: triage.Notification{GitHubID: "1"}},
	}
	testRepo(m).EXPECT().ListNotifications(mock.Anything).Return([]triage.NotificationWithState{
		{Notification: triage.Notification{GitHubID: "1"}, State: triage.State{IsReadLocally: false}},
	}, nil).Once()
	testRepo(m).EXPECT().MarkReadLocally(mock.Anything, "1", true).Return(nil).Once()
	testRepo(m).EXPECT().ListNotifications(mock.Anything).Return([]triage.NotificationWithState{
		{Notification: triage.Notification{GitHubID: "1"}, State: triage.State{IsReadLocally: true, IsHandledLocally: true}},
	}, nil).Once()

	cmd := m.MarkReadByID("1", true)
	require.NotNil(t, cmd)

	msg := executeCmd(cmd)
	reconciled, ok := msg.(mutationAppliedMsg)
	require.True(t, ok)
	assert.NoError(t, reconciled.err)

	actions := m.Transition(reconciled, 0)
	assert.True(t, m.allNotifications[0].IsReadLocally)
	assert.True(t, m.allNotifications[0].IsHandledLocally)
	assert.Nil(t, actions)
	assert.NoError(t, m.err)
}

func TestModel_MarkReadByID_StandaloneModeForwardsToGitHub(t *testing.T) {
	m := newTestModel(t)
	m.traffic = nil
	m.allNotifications = []triage.NotificationWithState{
		{Notification: triage.Notification{GitHubID: "1"}},
	}
	testRepo(m).EXPECT().ListNotifications(mock.Anything).Return([]triage.NotificationWithState{
		{Notification: triage.Notification{GitHubID: "1"}, State: triage.State{IsReadLocally: false}},
	}, nil).Once()
	testRepo(m).EXPECT().MarkReadLocally(mock.Anything, "1", true).Return(nil).Once()
	testClient(m).EXPECT().MarkThreadAsRead(mock.Anything, "1").Return(nil).Once()
	testRepo(m).EXPECT().ListNotifications(mock.Anything).Return([]triage.NotificationWithState{
		{Notification: triage.Notification{GitHubID: "1"}, State: triage.State{IsReadLocally: true, IsHandledLocally: true}},
	}, nil).Once()

	cmd := m.MarkReadByID("1", true)
	require.NotNil(t, cmd)

	msg := executeCmd(cmd)
	reconciled, ok := msg.(mutationAppliedMsg)
	require.True(t, ok)
	assert.NoError(t, reconciled.err)

	actions := m.Transition(reconciled, 0)
	assert.True(t, m.allNotifications[0].IsReadLocally)
	assert.True(t, m.allNotifications[0].IsHandledLocally)
	assert.Nil(t, actions)
	assert.NoError(t, m.err)
}

func TestModel_MarkReadByID_LocalFailureReconcilesToPersistedState(t *testing.T) {
	m := newTestModel(t)
	testBackend(m).Client = nil
	m.traffic = nil
	m.allNotifications = []triage.NotificationWithState{
		{Notification: triage.Notification{GitHubID: "1"}},
	}
	localErr := assert.AnError
	testRepo(m).EXPECT().ListNotifications(mock.Anything).Return([]triage.NotificationWithState{
		{Notification: triage.Notification{GitHubID: "1"}, State: triage.State{IsReadLocally: false}},
	}, nil).Once()
	testRepo(m).EXPECT().MarkReadLocally(mock.Anything, "1", true).Return(localErr).Once()
	testRepo(m).EXPECT().ListNotifications(mock.Anything).Return([]triage.NotificationWithState{
		{Notification: triage.Notification{GitHubID: "1"}, State: triage.State{IsReadLocally: false}},
	}, nil).Once()

	cmd := m.MarkReadByID("1", true)
	require.NotNil(t, cmd)

	msg := executeCmd(cmd)
	reconciled, ok := msg.(mutationAppliedMsg)
	require.True(t, ok)
	assert.ErrorIs(t, reconciled.err, localErr)

	actions := m.Transition(reconciled, 0)
	assert.False(t, m.allNotifications[0].IsReadLocally)
	assert.False(t, m.allNotifications[0].IsHandledLocally)
	assert.ErrorIs(t, m.err, localErr)
	assert.Contains(t, actions, ActionShowToast{Message: "Failed to update read state"})
}

func TestModel_MarkReadByID_LocalFailureReloadFailureRollsBackOptimisticState(t *testing.T) {
	m := newTestModel(t)
	testBackend(m).Client = nil
	m.traffic = nil
	m.allNotifications = []triage.NotificationWithState{
		{Notification: triage.Notification{GitHubID: "1"}},
	}
	localErr := assert.AnError
	reloadErr := sql.ErrConnDone
	testRepo(m).EXPECT().ListNotifications(mock.Anything).Return([]triage.NotificationWithState{
		{Notification: triage.Notification{GitHubID: "1"}, State: triage.State{IsReadLocally: false}},
	}, nil).Once()
	testRepo(m).EXPECT().MarkReadLocally(mock.Anything, "1", true).Return(localErr).Once()
	testRepo(m).EXPECT().ListNotifications(mock.Anything).Return(nil, reloadErr).Once()

	cmd := m.MarkReadByID("1", true)
	require.NotNil(t, cmd)

	msg := executeCmd(cmd)
	reconciled, ok := msg.(mutationAppliedMsg)
	require.True(t, ok)
	assert.ErrorIs(t, reconciled.err, localErr)
	actions := m.Transition(reconciled, 0)
	assert.False(t, m.allNotifications[0].IsReadLocally)
	assert.False(t, m.allNotifications[0].IsHandledLocally)
	assert.ErrorIs(t, m.err, localErr)
	assert.Contains(t, actions, ActionShowToast{Message: "Failed to update read state"})
}

func TestModel_MarkReadByID_RemoteFailureKeepsCommittedLocalState(t *testing.T) {
	m := newTestModel(t)
	m.traffic = nil
	m.allNotifications = []triage.NotificationWithState{
		{Notification: triage.Notification{GitHubID: "1"}},
	}
	remoteErr := assert.AnError
	testRepo(m).EXPECT().ListNotifications(mock.Anything).Return([]triage.NotificationWithState{
		{Notification: triage.Notification{GitHubID: "1"}, State: triage.State{IsReadLocally: false}},
	}, nil).Once()
	testRepo(m).EXPECT().MarkReadLocally(mock.Anything, "1", true).Return(nil).Once()
	testClient(m).EXPECT().MarkThreadAsRead(mock.Anything, "1").Return(remoteErr).Once()
	testRepo(m).EXPECT().ListNotifications(mock.Anything).Return([]triage.NotificationWithState{
		{Notification: triage.Notification{GitHubID: "1"}, State: triage.State{IsReadLocally: true, IsHandledLocally: true}},
	}, nil).Once()

	cmd := m.MarkReadByID("1", true)
	require.NotNil(t, cmd)

	msg := executeCmd(cmd)
	reconciled, ok := msg.(mutationAppliedMsg)
	require.True(t, ok)
	assert.ErrorIs(t, reconciled.err, remoteErr)

	actions := m.Transition(reconciled, 0)
	assert.True(t, m.allNotifications[0].IsReadLocally)
	assert.True(t, m.allNotifications[0].IsHandledLocally)
	assert.ErrorIs(t, m.err, remoteErr)
	assert.Contains(t, actions, ActionShowToast{Message: "Marked read locally; GitHub sync failed"})
}

func TestModel_Transition_EdgeCases(t *testing.T) {
	m := newTestModel(t)
	testRepo(m).EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()
	m.allNotifications = []triage.NotificationWithState{{Notification: triage.Notification{GitHubID: "1"}}}
	m.applyFilters()

	// 1. Detail Loaded (No actions returned, just state update)
	actions := m.Transition(detailLoadedMsg{GitHubID: "1", Body: "details"}, 0)
	assert.Equal(t, 0, len(actions))

	// 2. Priority Updated
	actions = m.Transition(mutationAppliedMsg{toast: "updated"}, 0)
	assert.Contains(t, actions, ActionShowToast{Message: "updated"})

	// 3. Sync Complete
	actions = m.Transition(syncCompleteMsg{rateLimit: models.RateLimitInfo{Remaining: 500}}, 0)
	assert.Contains(t, actions, ActionLoadNotifications{IsInitial: false, IsManual: false})
	assert.Equal(t, 500, m.RateLimit.Remaining)
}

func TestModel_HandlePollTick_StartsSyncingViaAction(t *testing.T) {
	m := newTestModel(t)
	m.heartbeatID = 7
	m.LastSyncAt = time.Now().Add(-time.Duration(m.PollInterval+1) * time.Second)

	actions := m.handlePollTick(pollTickMsg{ID: 7})
	assert.Contains(t, actions, ActionSetSyncing{Enabled: true})
	assert.Contains(t, actions, ActionSyncNotifications{Force: false, IsManual: false})
}

func TestModel_HandlePollTick_WithinIntervalDoesNotStartSyncing(t *testing.T) {
	m := newTestModel(t)
	m.heartbeatID = 7
	m.LastSyncAt = time.Now()

	actions := m.handlePollTick(pollTickMsg{ID: 7})
	assert.NotContains(t, actions, ActionSetSyncing{Enabled: true})
	assert.NotContains(t, actions, ActionSyncNotifications{Force: false, IsManual: false})
}

func TestModel_HandleSyncKey_AlreadyRunningShowsToast(t *testing.T) {
	m := newTestModel(t)
	m.ui.SetSyncing(true)

	actions := m.handleSyncKey()
	assert.Contains(t, actions, ActionShowToast{Message: "Sync already in progress"})
}

func TestModel_HandleSyncKey_StartsSyncingViaAction(t *testing.T) {
	m := newTestModel(t)

	actions := m.handleSyncKey()
	assert.Contains(t, actions, ActionSetSyncing{Enabled: true})
	assert.Contains(t, actions, ActionSyncNotifications{Force: true, IsManual: true})
}

func TestModel_HandleNotificationsLoaded_ManualNoChangeShowsToast(t *testing.T) {
	m := newTestModel(t)
	notifs := []triage.NotificationWithState{{Notification: triage.Notification{GitHubID: "1"}}}
	m.manualSyncPending = true
	m.manualSyncSnapshot = notificationStateSignature(notifs)

	actions := m.handleNotificationsLoaded(notificationsLoadedMsg{notifications: notifs, IsManual: true})
	assert.Contains(t, actions, ActionShowToast{Message: "No new notifications"})
	assert.False(t, m.manualSyncPending)
	assert.Empty(t, m.manualSyncSnapshot)
}

func TestModel_HandleNotificationsLoaded_ManualUpdatedShowsToast(t *testing.T) {
	m := newTestModel(t)
	before := []triage.NotificationWithState{{Notification: triage.Notification{GitHubID: "1"}}}
	after := []triage.NotificationWithState{{Notification: triage.Notification{GitHubID: "2"}}}
	m.manualSyncPending = true
	m.manualSyncSnapshot = notificationStateSignature(before)

	actions := m.handleNotificationsLoaded(notificationsLoadedMsg{notifications: after, IsManual: true})
	assert.Contains(t, actions, ActionShowToast{Message: "Sync complete"})
	assert.False(t, m.manualSyncPending)
	assert.Empty(t, m.manualSyncSnapshot)
}

func TestModel_HandleNotificationsLoaded_ManualForceEnrichesVisiblePage(t *testing.T) {
	m := newTestModel(t)
	fresh := triage.NotificationWithState{
		Notification: triage.Notification{
			GitHubID:    "fresh",
			SubjectType: triage.SubjectPullRequest,
			IsEnriched:  true,
			EnrichedAt:  sql.NullTime{Time: time.Now(), Valid: true},
			UpdatedAt:   time.Now(),
		},
	}

	actions := m.handleNotificationsLoaded(notificationsLoadedMsg{
		notifications: []triage.NotificationWithState{fresh},
		IsForced:      true,
		IsManual:      true,
	})

	require.Len(t, actions, 2)
	enrich, ok := actions[0].(ActionEnrichItems)
	require.True(t, ok)
	assert.True(t, enrich.Force)
	assert.Equal(t, []triage.NotificationWithState{fresh}, enrich.Notifications)
}

func TestModel_HandleNotificationsLoaded_BackgroundKeepsFreshTTLItems(t *testing.T) {
	m := newTestModel(t)
	fresh := triage.NotificationWithState{
		Notification: triage.Notification{
			GitHubID:    "fresh",
			SubjectType: triage.SubjectPullRequest,
			IsEnriched:  true,
			EnrichedAt:  sql.NullTime{Time: time.Now(), Valid: true},
			UpdatedAt:   time.Now(),
		},
	}

	actions := m.handleNotificationsLoaded(notificationsLoadedMsg{
		notifications: []triage.NotificationWithState{fresh},
		IsForced:      false,
		IsManual:      false,
	})

	require.Len(t, actions, 1)
	enrich, ok := actions[0].(ActionEnrichItems)
	require.True(t, ok)
	assert.False(t, enrich.Force)
	assert.Empty(t, enrich.Notifications)
}

func TestModel_ReloadEnrichmentCandidates_ManualDedupesSelectedVisibleItem(t *testing.T) {
	m := newTestModel(t)
	notifs := []triage.NotificationWithState{
		{Notification: triage.Notification{GitHubID: "selected", SubjectType: triage.SubjectPullRequest, IsEnriched: true, UpdatedAt: time.Now()}},
		{Notification: triage.Notification{GitHubID: "visible", SubjectType: triage.SubjectPullRequest, IsEnriched: true, UpdatedAt: time.Now()}},
	}

	m.allNotifications = notifs
	m.applyFilters()
	m.listView.list.Select(0)
	m.listView.list.Paginator.PerPage = 2

	candidates := m.getReloadEnrichmentCandidates(true, true)

	require.Len(t, candidates, 2)
	assert.Equal(t, "selected", candidates[0].GitHubID)
	assert.Equal(t, "visible", candidates[1].GitHubID)
}

func TestModel_FilterSelectedDetailRefreshCandidate_RemovesSelectedDuplicate(t *testing.T) {
	m := newTestModel(t)
	notifs := []triage.NotificationWithState{
		{Notification: triage.Notification{GitHubID: "selected", SubjectType: triage.SubjectPullRequest, IsEnriched: true, UpdatedAt: time.Now()}},
		{Notification: triage.Notification{GitHubID: "visible", SubjectType: triage.SubjectPullRequest, IsEnriched: true, UpdatedAt: time.Now()}},
	}

	m.allNotifications = notifs
	m.applyFilters()
	m.listView.list.Paginator.PerPage = 2
	m.listView.list.Select(0)
	m.state = StateDetail

	candidates := m.getReloadEnrichmentCandidates(true, true)
	filtered := m.filterSelectedDetailRefreshCandidate(candidates)

	require.Len(t, filtered, 1)
	assert.Equal(t, "visible", filtered[0].GitHubID)
}

func TestModel_HandleTransitionError_ManualSyncShowsFailureToast(t *testing.T) {
	m := newTestModel(t)
	m.manualSyncPending = true
	m.manualSyncSnapshot = "snapshot"

	actions := m.handleTransitionError(types.ErrMsg{Err: context.DeadlineExceeded})
	assert.Contains(t, actions, ActionShowToast{Message: "Sync failed"})
	assert.False(t, m.manualSyncPending)
	assert.Empty(t, m.manualSyncSnapshot)
}

func TestModel_Transition_Navigation(t *testing.T) {
	m := newTestModel(t)
	testRepo(m).EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()

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
	assert.Contains(t, actions, ActionSetFetching{Enabled: true})
	assert.Contains(t, actions, ActionEnrichItems{Notifications: []triage.NotificationWithState{notif}})

	// 2. Return to List View
	msg = keyPress("esc")
	_ = m.Transition(msg, 0)
	assert.Equal(t, StateList, m.state)
}

func TestModel_Transition_Priority(t *testing.T) {
	m := newTestModel(t)
	testRepo(m).EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()

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

func TestModel_Transition_DetailRefreshStartsFetchingViaAction(t *testing.T) {
	m := newTestModel(t)
	testRepo(m).EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()

	notif := triage.NotificationWithState{
		Notification: triage.Notification{
			GitHubID:    "1",
			SubjectURL:  "https://api.github.com/repos/o/r/issues/1",
			SubjectType: triage.SubjectIssue,
			IsEnriched:  true,
			UpdatedAt:   time.Now(),
		},
	}
	m.allNotifications = []triage.NotificationWithState{notif}
	m.applyFilters()
	m.listView.list.Select(0)
	m.state = StateDetail

	actions := m.Transition(keyPress("r"), 0)
	assert.Contains(t, actions, ActionSetSyncing{Enabled: true})
	assert.Contains(t, actions, ActionSyncNotifications{Force: true, IsManual: true})
	assert.Contains(t, actions, ActionSetFetching{Enabled: true})
	assert.Contains(t, actions, ActionFetchDetail{
		ID:          notif.GitHubID,
		URL:         notif.SubjectURL,
		SubjectType: notif.SubjectType,
		Force:       true,
	})
}

func TestModel_Transition_Tabs(t *testing.T) {
	m := newTestModel(t)
	testRepo(m).EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()

	// 1. Next Tab
	msg := keyPress("tab")
	initialTab := m.listView.activeTab
	_ = m.Transition(msg, 0)
	assert.Equal(t, (initialTab+1)%4, m.listView.activeTab)

	// 2. Prev Tab
	msg = keyPress("shift+tab")
	_ = m.Transition(msg, 0)
	assert.Equal(t, initialTab, m.listView.activeTab)

	m.setActiveTab(TabAll)
	_ = m.Transition(keyPress("tab"), 0)
	assert.Equal(t, TabInbox, m.listView.activeTab)

	_ = m.Transition(keyPress("shift+tab"), 0)
	assert.Equal(t, TabAll, m.listView.activeTab)

	_ = m.Transition(keyPress("1"), 0)
	assert.Equal(t, TabInbox, m.listView.activeTab)
	_ = m.Transition(keyPress("2"), 0)
	assert.Equal(t, TabTriaged, m.listView.activeTab)
	_ = m.Transition(keyPress("3"), 0)
	assert.Equal(t, TabAll, m.listView.activeTab)
}

func TestModel_ApplyFilters_ActiveTriageTabs(t *testing.T) {
	m := newTestModel(t)
	m.config.Notifications.MaxVisibleAgeDays = 0

	unread := triage.NotificationWithState{
		Notification: triage.Notification{GitHubID: "unread", UpdatedAt: time.Now()},
		State:        triage.State{IsReadLocally: true, IsHandledLocally: false, Priority: 0},
	}
	prioritizedRead := triage.NotificationWithState{
		Notification: triage.Notification{GitHubID: "prioritized-read", UpdatedAt: time.Now()},
		State:        triage.State{IsReadLocally: true, IsHandledLocally: true, Priority: 2},
	}
	done := triage.NotificationWithState{
		Notification: triage.Notification{GitHubID: "done", UpdatedAt: time.Now()},
		State:        triage.State{IsReadLocally: true, IsHandledLocally: true, Priority: 0},
	}
	m.allNotifications = []triage.NotificationWithState{unread, prioritizedRead, done}

	m.listView.activeTab = TabInbox
	m.applyFilters()
	assertListNotificationIDs(t, m, []string{"unread", "prioritized-read"})

	m.listView.activeTab = TabTriaged
	m.applyFilters()
	assertListNotificationIDs(t, m, []string{"prioritized-read"})

	m.listView.activeTab = TabAll
	m.applyFilters()
	assertListNotificationIDs(t, m, []string{"unread", "prioritized-read", "done"})
}

func TestModel_Transition_Enrichment(t *testing.T) {
	m := newTestModel(t)
	testRepo(m).EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()

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
	testRepo(m).EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()

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
	testRepo(m).EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()

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
	testRepo(m).EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()

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

func TestModel_ApplyFilters_HidesIgnoredRepositoriesAcrossTabs(t *testing.T) {
	m := newTestModel(t)
	m.config.Notifications.IgnoreRepos = []string{" hirakiuc/test-repo "}
	m.config.Notifications.MaxVisibleAgeDays = 0

	ignored := triage.NotificationWithState{
		Notification: triage.Notification{
			GitHubID:           "ignored",
			SubjectType:        triage.SubjectPullRequest,
			RepositoryFullName: "hirakiuc/test-repo",
			UpdatedAt:          time.Now(),
		},
		State: triage.State{
			Priority: 1,
		},
	}
	visible := triage.NotificationWithState{
		Notification: triage.Notification{
			GitHubID:           "visible",
			SubjectType:        triage.SubjectPullRequest,
			RepositoryFullName: "hirakiuc/kept-repo",
			UpdatedAt:          time.Now(),
		},
		State: triage.State{
			Priority: 1,
		},
	}
	m.allNotifications = []triage.NotificationWithState{ignored, visible}

	for _, tab := range []int{TabInbox, TabTriaged, TabAll} {
		m.listView.activeTab = tab
		m.applyFilters()

		require.Len(t, m.listView.list.Items(), 1, "tab %d should only show the non-ignored repo", tab)
		item, ok := m.listView.list.Items()[0].(item)
		require.True(t, ok)
		assert.Equal(t, "visible", item.notification.GitHubID)
	}
}

func TestModel_HandleNotificationsLoaded_KeepsIgnoredRepositoriesHiddenAfterReload(t *testing.T) {
	m := newTestModel(t)
	m.listView.activeTab = TabAll
	m.listView.list.SetSize(100, 100)
	m.config.Notifications.IgnoreRepos = []string{"hirakiuc/test-repo"}
	m.config.Notifications.MaxVisibleAgeDays = 0

	ignored := triage.NotificationWithState{
		Notification: triage.Notification{
			GitHubID:           "ignored",
			SubjectType:        triage.SubjectPullRequest,
			RepositoryFullName: "hirakiuc/test-repo",
			UpdatedAt:          time.Now(),
		},
	}
	visible := triage.NotificationWithState{
		Notification: triage.Notification{
			GitHubID:           "visible",
			SubjectType:        triage.SubjectPullRequest,
			RepositoryFullName: "hirakiuc/kept-repo",
			UpdatedAt:          time.Now(),
		},
	}

	actions := m.handleNotificationsLoaded(notificationsLoadedMsg{
		notifications: []triage.NotificationWithState{ignored, visible},
		IsManual:      true,
	})

	require.Len(t, m.listView.list.Items(), 1)
	item, ok := m.listView.list.Items()[0].(item)
	require.True(t, ok)
	assert.Equal(t, "visible", item.notification.GitHubID)

	for _, action := range actions {
		enrichAction, ok := action.(ActionEnrichItems)
		if !ok {
			continue
		}
		for _, n := range enrichAction.Notifications {
			assert.NotEqual(t, "ignored", n.GitHubID, "ignored repo should not be scheduled from visible-list enrichment")
		}
	}
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
	testRepo(m).EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()

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
