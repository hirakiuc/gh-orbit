package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/mocks"
	"github.com/hirakiuc/gh-orbit/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestModel_Transition_Core(t *testing.T) {
	m := newTestModel(t)
	m.db.(*mocks.MockRepository).EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()
	
	// 1. Initial Load
	notifs := []types.NotificationWithState{{Notification: types.Notification{GitHubID: "1"}}}
	actions := m.Transition(notificationsLoadedMsg{notifications: notifs, IsInitial: true}, 0)
	assert.Equal(t, notifs, m.allNotifications)
	require.Len(t, actions, 2)
	assert.IsType(t, ActionEnrichItems{}, actions[0])
	assert.IsType(t, ActionScheduleTick{}, actions[1])

	// 2. Window Size
	actions = m.Transition(tea.WindowSizeMsg{Width: 100, Height: 50}, 0)
	assert.Equal(t, 100, m.width)
	assert.Equal(t, 50, m.height)
	assert.Empty(t, actions)

	// 3. Toggle Read
	m.listView.list.Select(0)
	actions = m.Transition(tea.KeyPressMsg{Code: 'm', Text: "m"}, 0)
	require.Len(t, actions, 2)
	assert.IsType(t, ActionMarkRead{}, actions[0])
	assert.True(t, actions[0].(ActionMarkRead).Read)
	assert.IsType(t, ActionShowToast{}, actions[1])

	// 4. Sync Complete
	actions = m.Transition(syncCompleteMsg{rateLimit: types.RateLimitInfo{Remaining: 100}}, 0)
	require.Len(t, actions, 2)
	assert.IsType(t, ActionUpdateRateLimit{}, actions[0])
	assert.IsType(t, ActionLoadNotifications{}, actions[1])

	// 5. Double-Q Quit
	// First Q -> Toast
	actions = m.Transition(tea.KeyPressMsg{Code: 'q', Text: "q"}, 0)
	require.Len(t, actions, 1)
	assert.IsType(t, ActionShowToast{}, actions[0])
	assert.Equal(t, "Press q again to quit", actions[0].(ActionShowToast).Message)

	// Second Q (within 500ms) -> Quit
	actions = m.Transition(tea.KeyPressMsg{Code: 'q', Text: "q"}, 0)
	require.Len(t, actions, 1)
	assert.IsType(t, ActionQuit{}, actions[0])
}

func TestModel_Transition_Navigation(t *testing.T) {
	m := newTestModel(t)
	m.db.(*mocks.MockRepository).EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()
	m.allNotifications = []types.NotificationWithState{{Notification: types.Notification{GitHubID: "1"}}}
	m.applyFilters()
	m.listView.list.Select(0)

	// List -> Detail
	actions := m.Transition(tea.KeyPressMsg{Code: ' ', Text: " "}, 0)
	assert.Equal(t, StateDetail, m.state)
	require.Len(t, actions, 1)
	assert.IsType(t, ActionEnrichItems{}, actions[0])

	// Detail -> List
	actions = m.Transition(tea.KeyPressMsg{Code: tea.KeyEsc, Text: "esc"}, 0)
	assert.Equal(t, StateList, m.state)
	assert.Empty(t, actions)
}

func TestModel_Transition_Priorities(t *testing.T) {
	m := newTestModel(t)
	m.db.(*mocks.MockRepository).EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()
	m.allNotifications = []types.NotificationWithState{{
		Notification: types.Notification{GitHubID: "1"},
		OrbitState:   types.OrbitState{Priority: 0},
	}}
	m.applyFilters()
	m.listView.list.Select(0)

	// Shift+Up (Priority Up)
	actions := m.Transition(tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModShift}, 0)
	require.Len(t, actions, 2)
	assert.IsType(t, ActionSetPriority{}, actions[0])
	assert.Equal(t, 1, actions[0].(ActionSetPriority).Priority)
	assert.Equal(t, "Priority set to Low", actions[1].(ActionShowToast).Message)

	// '0' (Clear Priority)
	actions = m.Transition(tea.KeyPressMsg{Code: '0', Text: "0"}, 0)
	require.Len(t, actions, 2)
	assert.IsType(t, ActionSetPriority{}, actions[0])
	assert.Equal(t, 0, actions[0].(ActionSetPriority).Priority)
	assert.Equal(t, "Priority cleared", actions[1].(ActionShowToast).Message)
}

func TestModel_Transition_Filtering(t *testing.T) {
	m := newTestModel(t)
	m.db.(*mocks.MockRepository).EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()
	m.allNotifications = []types.NotificationWithState{
		{Notification: types.Notification{GitHubID: "1", SubjectType: "PullRequest"}},
		{Notification: types.Notification{GitHubID: "2", SubjectType: "Issue"}},
	}
	m.applyFilters()

	// Filter PRs
	m.Transition(tea.KeyPressMsg{Code: 'p', Text: "p"}, 0)
	assert.Equal(t, "PullRequest", m.listView.resourceFilter)
	assert.Equal(t, 1, len(m.listView.list.Items()))

	// Clear
	m.Transition(tea.KeyPressMsg{Code: 'p', Text: "p"}, 0)
	assert.Equal(t, "", m.listView.resourceFilter)
	assert.Equal(t, 2, len(m.listView.list.Items()))
}

func TestModel_Transition_Tabs(t *testing.T) {
	m := newTestModel(t)
	m.db.(*mocks.MockRepository).EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()
	
	// Tab 4 (All)
	m.Transition(tea.KeyPressMsg{Code: '4', Text: "4"}, 0)
	assert.Equal(t, TabAll, m.listView.activeTab)

	// Tab 1 (Inbox)
	m.Transition(tea.KeyPressMsg{Code: '1', Text: "1"}, 0)
	assert.Equal(t, TabInbox, m.listView.activeTab)

	// Next Tab (])
	m.Transition(tea.KeyPressMsg{Code: ']', Text: "]"}, 0)
	assert.Equal(t, TabUnread, m.listView.activeTab)
}

func TestInterpreter_FullFlow(t *testing.T) {
	m := newTestModel(t)
	interp := NewInterpreter(m)
	
	// Mock Submit for all actions that use it
	m.traffic.(*mocks.MockTrafficController).EXPECT().Submit(mock.Anything, mock.Anything).Return(func() tea.Msg { return nil }).Maybe()
	m.traffic.(*mocks.MockTrafficController).EXPECT().UpdateRateLimit(mock.Anything, mock.Anything).Return().Maybe()
	
	mockExecutor := m.executor.(*mocks.MockCommandExecutor)
	mockExecutor.EXPECT().InteractiveGH(mock.Anything, "pr", "checkout", "1", "-R", "o/r").Return(func() tea.Msg { return nil }).Maybe()
	mockExecutor.EXPECT().Run(mock.Anything, "gh", "pr", "view", "1", "-R", "o/r", "--web").Return(nil).Maybe()
	
	notif := types.NotificationWithState{
		Notification: types.Notification{
			GitHubID: "1",
			SubjectURL: "https://api.github.com/repos/o/r/pulls/1",
			SubjectType: "PullRequest",
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
		ActionEnrichItems{Notifications: []types.NotificationWithState{notif}},
		ActionLoadNotifications{},
		ActionUpdateRateLimit{Info: types.RateLimitInfo{Remaining: 100}},
		ActionScheduleTick{TickType: TickHeartbeat, Interval: time.Millisecond},
		ActionScheduleTick{TickType: TickClock, Interval: time.Millisecond},
		ActionScheduleTick{TickType: TickToast, Interval: time.Millisecond},
	}
	
	for _, a := range actions {
		cmd := interp.Execute(a)
		assert.NotNil(t, cmd)
		if a.Type() == ActionTypeShowToast {
			_ = executeCmd(cmd)
			assert.Equal(t, "msg", m.ui.toastMessage)
		}
	}
}

func TestModel_Transition_EdgeCases(t *testing.T) {
	m := newTestModel(t)
	m.db.(*mocks.MockRepository).EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()
	m.allNotifications = []types.NotificationWithState{{Notification: types.Notification{GitHubID: "1"}}}
	m.applyFilters()

	// 1. Detail Loaded
	actions := m.Transition(detailLoadedMsg{GitHubID: "1", Body: "details"}, 0)
	assert.True(t, m.allNotifications[0].IsEnriched)
	assert.Equal(t, "details", m.allNotifications[0].Body)
	assert.Empty(t, actions)

	// 2. Poll Tick (Trigger sync)
	m.LastSyncAt = time.Now().Add(-1 * time.Hour)
	m.PollInterval = 60
	actions = m.Transition(pollTickMsg{ID: m.heartbeatID}, 0)
	require.Len(t, actions, 2)
	assert.IsType(t, ActionSyncNotifications{}, actions[0])
	assert.IsType(t, ActionScheduleTick{}, actions[1])

	// 3. Error Msg
	actions = m.Transition(types.ErrMsg{Err: fmt.Errorf("fail")}, 0)
	assert.NotNil(t, m.err)
	assert.Empty(t, actions)
}

func TestInterpreter_SpecialCases(t *testing.T) {
	m := newTestModel(t)
	interp := NewInterpreter(m)
	
	// ActionEnrichItems with multiple notifications
	notifs := []types.NotificationWithState{
		{Notification: types.Notification{GitHubID: "1"}},
		{Notification: types.Notification{GitHubID: "2"}},
	}
	
	mockTraffic := m.traffic.(*mocks.MockTrafficController)
	mockTraffic.EXPECT().Submit(mock.Anything, mock.Anything).Return(func() tea.Msg { return nil }).Maybe()
	
	cmd := interp.Execute(ActionEnrichItems{Notifications: notifs})
	assert.NotNil(t, cmd)
}

func TestModel_Actions_Functional(t *testing.T) {
	m := newTestModel(t)
	
	// Override specific methods we need to verify precisely
	mockRepo := m.db.(*mocks.MockRepository)
	mockClient := m.client.(*mocks.MockGitHubClient)
	mockEnricher := m.enrich.(*mocks.MockEnricher)
	mockTraffic := m.traffic.(*mocks.MockTrafficController)

	// Ensure we match and execute the submitted function
	mockTraffic.EXPECT().Submit(mock.Anything, mock.Anything).RunAndReturn(func(p int, fn types.TaskFunc) tea.Cmd {
		return func() tea.Msg { return fn(context.Background()) }
	}).Maybe()

	// 1. MarkReadByID
	mockRepo.EXPECT().MarkReadLocally(mock.Anything, "1", true).Return(nil).Once()
	mockClient.EXPECT().MarkThreadAsRead(mock.Anything, "1").Return(nil).Once()
	mockRepo.EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()
	
	_ = executeCmd(m.MarkReadByID("1", true))
	
	// 2. setPriorityByID
	mockRepo.EXPECT().SetPriority(mock.Anything, "1", 2).Return(nil).Once()
	
	_ = executeCmd(m.setPriorityByID("1", 2))
	
	// 3. enrichItems (Viewport)
	notifs := []types.NotificationWithState{
		{Notification: types.Notification{GitHubID: "1"}},
		{Notification: types.Notification{GitHubID: "2"}},
	}
	mockEnricher.EXPECT().FetchHybridBatch(mock.Anything, mock.Anything).Return(nil).Once()
	
	_ = executeCmd(m.enrichItems(notifs))
}

func TestModel_Update_FullShell(t *testing.T) {
	m := newTestModel(t)
	m.db.(*mocks.MockRepository).EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()
	
	// Test that Update actually executes an action (ShowToast)
	msgSync := priorityUpdatedMsg{toast: "hello"}
	_, cmd := m.Update(msgSync)
	require.NotNil(t, cmd)
	_ = executeCmd(cmd)
	assert.Equal(t, "hello", m.ui.toastMessage)
}

func TestModel_Transition_DetailKeys(t *testing.T) {
	m := newTestModel(t)
	m.db.(*mocks.MockRepository).EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()
	notif := types.NotificationWithState{
		Notification: types.Notification{GitHubID: "1", SubjectType: "PullRequest", SubjectURL: "https://api.github.com/repos/o/r/pulls/1"},
	}
	m.allNotifications = []types.NotificationWithState{notif}
	m.applyFilters()
	m.listView.list.Select(0)

	// Enter Detail
	m.Transition(tea.KeyPressMsg{Code: ' ', Text: " "}, 0)
	assert.Equal(t, StateDetail, m.state)

	// Detail: Open Browser
	actions := m.Transition(tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"}, 0)
	require.Len(t, actions, 1)
	assert.IsType(t, ActionViewWeb{}, actions[0])

	// Detail: Checkout PR
	actions = m.Transition(tea.KeyPressMsg{Code: 'c', Text: "c"}, 0)
	require.Len(t, actions, 1)
	assert.IsType(t, ActionCheckoutPR{}, actions[0])

	// Detail: Back
	m.Transition(tea.KeyPressMsg{Code: tea.KeyEsc, Text: "esc"}, 0)
	assert.Equal(t, StateList, m.state)
}

func TestModel_Transition_Debounce(t *testing.T) {
	m := newTestModel(t)
	m.db.(*mocks.MockRepository).EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()
	m.allNotifications = []types.NotificationWithState{
		{Notification: types.Notification{GitHubID: "1"}},
		{Notification: types.Notification{GitHubID: "2"}},
	}
	m.applyFilters()
	m.listView.list.Select(0)

	// Simulate index change to trigger debounce tick
	m.listView.list.Select(1)
	actions := m.Transition(tea.KeyPressMsg{Code: tea.KeyDown}, 0)
	
	found := false
	for _, a := range actions {
		if ta, ok := a.(ActionScheduleTick); ok && ta.TickType == TickEnrich {
			found = true
		}
	}
	assert.True(t, found, "Expected debounce tick action")
}

func TestModel_View_Comprehensive(t *testing.T) {
	m := newTestModel(t)
	m.Transition(tea.WindowSizeMsg{Width: 100, Height: 50}, 0)
	
	// List View
	v1 := m.View()
	assert.NotEmpty(t, v1.Content)
	
	// Detail View
	m.state = StateDetail
	v2 := m.View()
	assert.NotEmpty(t, v2.Content)

	// Error View
	m.err = fmt.Errorf("test error")
	v3 := m.View()
	assert.Contains(t, stripANSI(v3.Content), "Error: test error")
}

func TestURLHelpers(t *testing.T) {
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

func TestModel_SyncNotifications(t *testing.T) {
	m := newTestModel(t)
	
	mockSyncer := m.sync.(*mocks.MockSyncer)
	mockSyncer.EXPECT().Sync(mock.Anything, "test-user", true).Return(types.RateLimitInfo{Remaining: 100}, nil).Once()
	mockSyncer.EXPECT().Sync(mock.Anything, "test-user", false).Return(types.RateLimitInfo{Remaining: 200}, nil).Once()
	
	mockTraffic := m.traffic.(*mocks.MockTrafficController)
	mockTraffic.EXPECT().Submit(mock.Anything, mock.Anything).RunAndReturn(func(priority int, fn types.TaskFunc) tea.Cmd {
		return func() tea.Msg { return fn(context.Background()) }
	}).Twice()
	
	// Test forced sync
	cmd1 := m.syncNotificationsWithForce(true)
	msg1 := executeCmd(cmd1)
	assert.IsType(t, syncCompleteMsg{}, msg1)
	assert.Equal(t, 100, msg1.(syncCompleteMsg).rateLimit.Remaining)
	
	// Test background sync
	cmd2 := m.syncNotificationsWithForce(false)
	msg2 := executeCmd(cmd2)
	assert.IsType(t, syncCompleteMsg{}, msg2)
	assert.Equal(t, 200, msg2.(syncCompleteMsg).rateLimit.Remaining)
}

func TestModel_GHViewCmd_Validation(t *testing.T) {
	m := newTestModel(t)
	
	// Invalid repo
	cmd := m.ghViewCmd("pr", "invalid-repo", "1")
	msg := executeCmd(cmd)
	require.IsType(t, types.ErrMsg{}, msg)
	
	// Invalid release tag
	cmd2 := m.ghViewCmd("release", "o/r", "invalid tag!")
	msg2 := executeCmd(cmd2)
	require.IsType(t, types.ErrMsg{}, msg2)
	
	// Invalid number
	cmd3 := m.ghViewCmd("pr", "o/r", "abc")
	msg3 := executeCmd(cmd3)
	require.IsType(t, types.ErrMsg{}, msg3)
}

func TestModel_DetailView_ContentSync(t *testing.T) {
	m := newTestModel(t)
	m.db.(*mocks.MockRepository).EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()
	
	// Initialize renderer
	m.width = 100
	m.updateMarkdownRenderer()
	
	notif := types.NotificationWithState{
		Notification: types.Notification{
			GitHubID: "1", 
			SubjectTitle: "T1", 
			Body: "original body", 
			IsEnriched: true,
		},
	}
	m.allNotifications = []types.NotificationWithState{notif}
	m.applyFilters()
	m.listView.list.Select(0)

	// 1. Entering detail for pre-enriched item should show body immediately
	m.Transition(tea.KeyPressMsg{Code: ' ', Text: " "}, 0)
	assert.Equal(t, StateDetail, m.state)
	assert.Contains(t, stripANSI(m.detailView.activeDetail), "original body")

	// 2. Loading new details while in detail view should refresh content
	m.Transition(detailLoadedMsg{GitHubID: "1", Body: "updated body"}, 0)
	assert.Contains(t, stripANSI(m.detailView.activeDetail), "updated body")

	// 3. Background sync while in detail view should preserve/refresh content
	newNotifs := []types.NotificationWithState{
		{
			Notification: types.Notification{
				GitHubID: "1", 
				SubjectTitle: "T1", 
				Body: "synced body", 
				IsEnriched: true,
			},
		},
	}
	m.Transition(notificationsLoadedMsg{notifications: newNotifs, IsInitial: true}, 0)
	assert.Contains(t, stripANSI(m.detailView.activeDetail), "synced body")
}

func TestModel_DetailView_Scrolling(t *testing.T) {
	m := newTestModel(t)
	m.db.(*mocks.MockRepository).EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()
	
	// Set dimensions and initialize renderer
	m.width = 80
	m.height = 24
	m.updateMarkdownRenderer()
	
	// Long body to ensure scrolling is possible
	longBody := strings.Repeat("Line of text\n", 50)
	notif := types.NotificationWithState{
		Notification: types.Notification{
			GitHubID: "1", 
			SubjectTitle: "T1", 
			Body: longBody, 
			IsEnriched: true,
		},
	}
	m.allNotifications = []types.NotificationWithState{notif}
	m.applyFilters()
	m.listView.list.Select(0)

	// 1. Enter detail view
	m.Transition(tea.KeyPressMsg{Code: ' ', Text: " "}, 0)
	assert.Equal(t, StateDetail, m.state)
	m.refreshDetailView() // Force refresh to ensure viewport is populated

	initialOffset := m.detailView.viewport.YOffset()
	assert.Equal(t, 0, initialOffset)

	// 2. Simulate scroll down (j key)
	// We call Update directly to simulate sub-model delegation
	_, _ = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	
	newOffset := m.detailView.viewport.YOffset()
	assert.Greater(t, newOffset, initialOffset, "Viewport YOffset should increase after scroll down")
}

func TestModel_Actions_EdgeCases(t *testing.T) {
	m := newTestModel(t)
	m.db.(*mocks.MockRepository).EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()
	
	// 1. OpenBrowser with empty URL
	cmd := m.OpenBrowser("")
	assert.Nil(t, cmd)
	
	// 2. OpenBrowser with invalid URL
	cmd2 := m.OpenBrowser("http://google.com") // not github.com
	msg := executeCmd(cmd2)
	assert.IsType(t, types.ErrMsg{}, msg)
	
	// 3. ViewItem with unknown type
	notif := types.NotificationWithState{
		Notification: types.Notification{SubjectType: "Unknown", HTMLURL: "https://github.com/o/r"},
	}
	mockTraffic := m.traffic.(*mocks.MockTrafficController)
	mockTraffic.EXPECT().Submit(mock.Anything, mock.Anything).Return(func() tea.Msg { return nil }).Maybe()
	
	_ = m.ViewItem(item{notification: notif})
	assert.Equal(t, "Opening in browser...", m.ui.toastMessage)
}

func TestModel_Options(t *testing.T) {
	cfg := &config.Config{}
	m := NewModel(
		"u", cfg, nil, nil, nil, nil, nil, nil, nil,
		WithTheme(false),
		WithVersion("1.2.3"),
		WithExecutor(mocks.NewMockCommandExecutor(t)),
	)
	
	assert.False(t, m.isDark)
	assert.Equal(t, "1.2.3", m.version)
}

func TestModel_Delegate(t *testing.T) {
	notif := types.NotificationWithState{
		Notification: types.Notification{
			SubjectTitle:       "Title",
			RepositoryFullName: "o/r",
			ResourceState:      "open",
		},
	}
	i := item{notification: notif}
	
	assert.Equal(t, "Title", i.Title())
	assert.Equal(t, "o/r", i.Description())
	assert.Equal(t, "Title o/r open", i.FilterValue())
}

func TestModel_Shutdown(t *testing.T) {
	m := newTestModel(t)
	
	mockSyncer := m.sync.(*mocks.MockSyncer)
	mockSyncer.EXPECT().Shutdown(mock.Anything).Return().Once()
	
	mockTraffic := m.traffic.(*mocks.MockTrafficController)
	mockTraffic.EXPECT().Shutdown(mock.Anything).Return().Once()
	
	mockAlerter := m.alerter.(*mocks.MockAlerter)
	mockAlerter.EXPECT().Shutdown(mock.Anything).Return().Once()
	
	m.Shutdown()
}

func TestModel_Init(t *testing.T) {
	m := newTestModel(t)
	m.db.(*mocks.MockRepository).EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()
	
	cmd := m.Init()
	require.NotNil(t, cmd)
	_ = executeCmd(cmd)
}

func TestModel_FetchDetailCmd(t *testing.T) {
	m := newTestModel(t)
	m.db.(*mocks.MockRepository).EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()

	mockTraffic := m.traffic.(*mocks.MockTrafficController)
	mockTraffic.EXPECT().Submit(mock.Anything, mock.Anything).RunAndReturn(func(p int, fn types.TaskFunc) tea.Cmd {
		return func() tea.Msg { return fn(context.Background()) }
	}).Once()

	mockEnricher := m.enrich.(*mocks.MockEnricher)
	mockEnricher.EXPECT().FetchDetail(mock.Anything, "url", "type").Return(types.EnrichmentResult{
		Body:          "body",
		Author:        "author",
		HTMLURL:       "html_url",
		ResourceState: "OPEN",
	}, nil).Once()

	mockRepo := m.db.(*mocks.MockRepository)
	mockRepo.EXPECT().EnrichNotification(mock.Anything, "id", "body", "author", "html_url", "OPEN").Return(nil).Once()

	cmd := m.FetchDetailCmd("id", "url", "type")
	require.NotNil(t, cmd)

	msg := executeCmd(cmd)
	assert.IsType(t, detailLoadedMsg{}, msg)
	dMsg := msg.(detailLoadedMsg)
	assert.Equal(t, "id", dMsg.GitHubID)
	assert.Equal(t, "body", dMsg.Body)
}

func TestModel_LoadNotifications(t *testing.T) {
	m := newTestModel(t)

	mockRepo := m.db.(*mocks.MockRepository)
	notifs := []types.NotificationWithState{
		{Notification: types.Notification{GitHubID: "1"}},
	}
	mockRepo.EXPECT().ListNotifications(mock.Anything).Return(notifs, nil).Once()

	cmd := m.loadNotifications()
	require.NotNil(t, cmd)

	msg := executeCmd(cmd)
	assert.IsType(t, notificationsLoadedMsg{}, msg)
	assert.Len(t, msg.(notificationsLoadedMsg).notifications, 1)
	assert.True(t, msg.(notificationsLoadedMsg).IsInitial)

	// Error path
	mockRepo.EXPECT().ListNotifications(mock.Anything).Return(nil, fmt.Errorf("db error")).Once()
	cmdErr := m.loadNotifications()
	msgErr := executeCmd(cmdErr)
	assert.IsType(t, types.ErrMsg{}, msgErr)
}
func TestUIController_Comprehensive(t *testing.T) {
	styles := DefaultStyles(true)
	ui := NewUIController(styles)

	// SetStyles coverage
	ui.SetStyles(DefaultStyles(false))

	// Update with clearStatusMsg
	ui.SetToast("toast")
	ui, _ = ui.Update(clearStatusMsg{})
	assert.Empty(t, ui.toastMessage)

	// Update with spinner.TickMsg (not fetching/syncing)
	ui, _ = ui.Update(spinner.TickMsg{})

	// Update with spinner.TickMsg (fetching)
	ui.SetFetching(true)
	ui, _ = ui.Update(spinner.TickMsg{Time: time.Now()})

	ui.SetSize(100, 20)

	// View coverage - base
	v := ui.View("base", false, 0, 0, 0)
	assert.Contains(t, v, "base")

	// View - Toast & Syncing
	ui.SetToast("toast")
	ui.SetSyncing(true)
	v = ui.View("base", false, 0, 0, 0)
	assert.Contains(t, stripANSI(v), "toast")

	// View - Detail Scrollbar
	ui.SetFetching(false)
	v = ui.View("base", true, 0.5, 10, 100)
	assert.NotEmpty(t, v)

	// View - Detail Scrollbar Edge Case
	v = ui.View("base", true, 0.0, 10, 0) // totalLines=0
	assert.NotEmpty(t, v)

	// View - Filter Chip
	ui.SetResourceFilter("PRs")
	v = ui.View("base", false, 0, 0, 0)
	assert.Contains(t, stripANSI(v), "FILTER: PRS")

	// RenderSpinner coverage
	assert.NotEmpty(t, ui.RenderSpinner())
}

func TestModel_Actions_Additional(t *testing.T) {
	m := newTestModel(t)
	m.db.(*mocks.MockRepository).EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()
	
	notif := types.NotificationWithState{
		Notification: types.Notification{
			GitHubID: "1",
			RepositoryFullName: "o/r",
			SubjectURL: "https://api.github.com/repos/o/r/issues/1",
		},
	}
	i := item{notification: notif}
	m.allNotifications = []types.NotificationWithState{notif}
	m.applyFilters()
	m.listView.list.Select(0)

	mockTraffic := m.traffic.(*mocks.MockTrafficController)
	mockTraffic.EXPECT().Submit(mock.Anything, mock.Anything).Return(func() tea.Msg { return nil }).Maybe()

	// 1. ToggleRead (via method)
	_ = m.ToggleRead(i)
	
	// 2. ViewIssueWeb
	_ = m.ViewIssueWeb("o/r", "1")
	
	// 3. ViewReleaseWeb
	_ = m.ViewReleaseWeb("o/r", "v1.0")
	
	// 4. MarkRead (via method)
	_ = m.MarkRead(i)
}
