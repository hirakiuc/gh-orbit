package tui

import (
	"context"
	"fmt"
	"image/color"
	"log/slog"
	"testing"

	"charm.land/lipgloss/v2"
	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/mocks"
	"github.com/hirakiuc/gh-orbit/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func newTestModel(t testing.TB) *Model {
	cfg := &config.Config{}
	logger := slog.Default()
	userID := "test-user"

	// Mock engines via the new centralized interfaces
	mockSyncer := mocks.NewMockSyncer(t)
	mockSyncer.EXPECT().BridgeStatus().Return(types.StatusHealthy).Maybe()
	mockEnricher := mocks.NewMockEnricher(t)
	mockTraffic := mocks.NewMockTrafficController(t)
	mockTraffic.EXPECT().Remaining().Return(5000).Maybe()
	mockAlerter := mocks.NewMockAlerter(t)
	mockAlerter.EXPECT().BridgeStatus().Return(types.StatusHealthy).Maybe()
	mockRepo := mocks.NewMockRepository(t)
	mockClient := mocks.NewMockGitHubClient(t)

	m := NewModel(
		userID,
		cfg,
		logger,
		mockRepo,
		mockClient,
		mockSyncer,
		mockEnricher,
		mockTraffic,
		mockAlerter,
	)
	
	m.ui.SetSize(80, 24)
	return m
}

func TestModel_Update_SyncingState(t *testing.T) {
	m := newTestModel(t)
	m.ui.SetSyncing(true)

	// syncCompleteMsg SHOULD reset syncing and update rate limit
	msg := syncCompleteMsg{remainingRateLimit: 4500}
	
	mockTraffic := m.traffic.(*mocks.MockTrafficController)
	mockTraffic.EXPECT().UpdateRateLimit(mock.Anything, 4500).Return().Once()

	rawModel, _ := m.Update(msg)
	updatedModel := rawModel.(*Model)
	assert.False(t, updatedModel.ui.syncing, "expected syncing to be false after syncCompleteMsg")

	// Test error reset
	m.ui.SetSyncing(true)
	msgErr := errMsg{err: nil}
	rawModel, _ = m.Update(msgErr)
	updatedModel = rawModel.(*Model)
	assert.False(t, updatedModel.ui.syncing, "expected syncing to be false after errMsg")
}

func TestModel_Update_ThemeChange(t *testing.T) {
	m := newTestModel(t)

	// Mock a light background color msg
	msg := tea.BackgroundColorMsg{Color: color.RGBA{R: 255, G: 255, B: 255, A: 255}}
	_, _ = m.Update(msg)
}

func TestModel_Update_StatusClearing(t *testing.T) {
	m := newTestModel(t)
	m.ui.SetToast("temporary status")

	msgClear := clearStatusMsg{}
	updatedModel, _ := m.Update(msgClear)
	newModel := updatedModel.(*Model)
	assert.Empty(t, newModel.ui.toastMessage, "expected status to be cleared after clearStatusMsg")
}

func TestModel_Update_TableDriven(t *testing.T) {
	tests := map[string]struct {
		setup func(*Model)
		msg   tea.Msg
		verify func(*testing.T, *Model)
	}{
		"Tab Change: All (Key 4)": {
			setup: func(m *Model) { m.listView.activeTab = TabInbox },
			msg:   tea.KeyPressMsg{Text: "4", Code: '4'},
			verify: func(t *testing.T, m *Model) { assert.Equal(t, TabAll, m.listView.activeTab) },
		},
		"Error Message": {
			msg: errMsg{err: fmt.Errorf("fatal")},
			verify: func(t *testing.T, m *Model) { assert.Equal(t, "fatal", m.err.Error()) },
		},
		"Action Complete": {
			msg: actionCompleteMsg{},
			verify: func(t *testing.T, m *Model) { /* just verify no crash */ },
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			m := newTestModel(t)
			if tc.setup != nil {
				tc.setup(m)
			}
			updatedModel, _ := m.Update(tc.msg)
			tc.verify(t, updatedModel.(*Model))
		})
	}
}

func TestModel_View_States(t *testing.T) {
	m := newTestModel(t)
	
	// 1. List State
	m.state = StateList
	v1 := m.View()
	assert.Contains(t, stripANSI(v1.Content), "Inbox") // Header tab

	// 2. Detail State
	m.state = StateDetail
	m.allNotifications = []types.NotificationWithState{{
		Notification: types.Notification{SubjectTitle: "Detail Title", GitHubID: "1"},
	}}
	m.applyFilters()
	m.listView.list.Select(0)
	
	v2 := m.View()
	assert.Contains(t, stripANSI(v2.Content), "Detail Title")
}

func TestModel_MarkRead(t *testing.T) {
	m := newTestModel(t)
	m.allNotifications = []types.NotificationWithState{{
		Notification: types.Notification{GitHubID: "test-id"},
		OrbitState:   types.OrbitState{IsReadLocally: false},
	}}
	m.listView.activeTab = TabAll
	
	// Mock expectations
	mockRepo := m.db.(*mocks.MockRepository)
	mockClient := m.client.(*mocks.MockGitHubClient)
	mockTraffic := m.traffic.(*mocks.MockTrafficController)
	
	// TUI actions now route through Traffic Controller
	mockTraffic.EXPECT().Submit(mock.Anything, mock.Anything).RunAndReturn(func(priority int, fn func(context.Context) tea.Msg) tea.Cmd {
		return func() tea.Msg {
			// Actually execute the function to trigger expectations
			_ = fn(context.Background())
			return nil
		}
	}).Once()

	// The functions are executed INSIDE the Submit callback, so we expect them on the mocks
	mockRepo.EXPECT().MarkReadLocally(mock.Anything, "test-id", true).Return(nil).Once()
	mockClient.EXPECT().MarkThreadAsRead(mock.Anything, "test-id").Return(nil).Once()

	m.applyFilters()

	// Initial state
	items := m.listView.list.Items()
	require.NotEmpty(t, items)
	
	i := items[0].(item)
	cmd := m.MarkRead(i)
	require.NotNil(t, cmd)
	
	// Execute cmd to verify submission and internal calls
	msg := cmd()
	require.Nil(t, msg)

	assert.True(t, m.allNotifications[0].IsReadLocally, "expected allNotifications[0] to be marked as read")
}

func TestModel_ResourceFiltering(t *testing.T) {
	m := newTestModel(t)
	m.allNotifications = []types.NotificationWithState{
		{Notification: types.Notification{GitHubID: "1", SubjectType: "PullRequest"}},
		{Notification: types.Notification{GitHubID: "2", SubjectType: "Issue"}},
	}
	m.listView.activeTab = TabAll

	// 1. Filter PRs
	m.toggleResourceFilter("PullRequest", "PRs")
	assert.Equal(t, 1, len(m.listView.list.Items()), "expected 1 item (PR)")

	// 2. Clear
	m.toggleResourceFilter("PullRequest", "PRs")
	assert.Equal(t, 2, len(m.listView.list.Items()), "expected 2 items after clear")
}

func TestModel_Navigation(t *testing.T) {
	m := newTestModel(t)

	// 1. Test Detail -> List transition via 'q'
	m.state = StateDetail
	msgQ := tea.KeyPressMsg{Text: "q", Code: 'q'}
	model, _ := m.Update(msgQ)
	assert.Equal(t, StateList, model.(*Model).state, "expected StateList after pressing 'q' in StateDetail")

	// 2. Test Quit transition via 'q'
	m.listView.list.Help.ShowAll = false
	_, cmd := m.Update(msgQ)
	assert.NotNil(t, cmd, "expected quit command, got nil")
}

func TestRenderTargetHeader_Geometry(t *testing.T) {
	styles := DefaultStyles(true)
	ctx := RenderContext{
		Styles: styles,
		Width:  40, // Small width to force truncation
	}
	notif := types.NotificationWithState{
		Notification: types.Notification{
			SubjectTitle: "A very long title that should be truncated when a badge is present",
			SubjectType:  "PullRequest",
		},
	}

	// 1. Un-enriched (No badge)
	h1 := RenderTargetHeader(ctx, notif, "", false)
	assert.LessOrEqual(t, lipgloss.Width(h1), ctx.Width, "Header (No Badge) should not exceed width")

	// 2. Fetching (Skeleton badge)
	ctx.IsFetching = true
	h2 := RenderTargetHeader(ctx, notif, "", false)
	assert.Contains(t, stripANSI(h2), "FETCH")
	assert.LessOrEqual(t, lipgloss.Width(h2), ctx.Width, "Header (Fetching) should not exceed width")

	// 3. Enriched (Status badge)
	ctx.IsFetching = false
	notif.ResourceState = "Merged"
	h3 := RenderTargetHeader(ctx, notif, "", false)
	assert.Contains(t, stripANSI(h3), "MERGED")
	assert.LessOrEqual(t, lipgloss.Width(h3), ctx.Width, "Header (Merged) should not exceed width")
}
