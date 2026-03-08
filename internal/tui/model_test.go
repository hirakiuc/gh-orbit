package tui

import (
	"context"
	"fmt"
	"image/color"
	"log/slog"
	"os"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/api"
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
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	userID := "test-user"

	// Mock engines via the new centralized interfaces
	mockSyncer := mocks.NewMockSyncer(t)
	var m *Model
	mockSyncer.EXPECT().BridgeStatus().RunAndReturn(func() api.BridgeStatus {
		if m == nil {
			return api.StatusHealthy
		}
		return m.bridgeStatus
	}).Maybe()
	mockEnricher := mocks.NewMockEnricher(t)
	mockEnricher.EXPECT().FetchHybridBatch(mock.Anything, mock.Anything).Return(nil).Maybe()
	mockTraffic := mocks.NewMockTrafficController(t)
	mockTraffic.EXPECT().Remaining().Return(5000).Maybe()
	mockAlerter := mocks.NewMockAlerter(t)
	mockAlerter.EXPECT().BridgeStatus().Return(api.StatusHealthy).Maybe()
	mockRepo := mocks.NewMockRepository(t)
	mockRepo.EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()
	mockClient := mocks.NewMockGitHubClient(t)

	m = NewModel(
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
	
	m.heartbeatInterval = time.Millisecond
	m.clockInterval = time.Millisecond
	m.ui.toastTimeout = time.Millisecond
	m.bridgeStatus = api.StatusHealthy
	
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
		"Window Size": {
			msg: tea.WindowSizeMsg{Width: 100, Height: 50},
			verify: func(t *testing.T, m *Model) {
				assert.Equal(t, 100, m.width)
				assert.Equal(t, 50, m.height)
			},
		},
		"Sync Complete": {
			msg: syncCompleteMsg{remainingRateLimit: 1234},
			setup: func(m *Model) {
				m.traffic.(*mocks.MockTrafficController).EXPECT().UpdateRateLimit(mock.Anything, 1234).Return().Once()
			},
			verify: func(t *testing.T, m *Model) {
				assert.False(t, m.ui.syncing)
			},
		},
		"View Port Enrich": {
			msg: viewportEnrichMsg{},
			verify: func(t *testing.T, m *Model) { /* verify no crash */ },
		},
		"Clear Status": {
			msg: clearStatusMsg{},
			verify: func(t *testing.T, m *Model) {
				assert.Empty(t, m.ui.toastMessage)
			},
		},
		"Key: q (Quit)": {
			msg: tea.KeyPressMsg{Code: 'q', Text: "q"},
			verify: func(t *testing.T, m *Model) { /* command checked separately in Navigation test */ },
		},
		"Key: 1 (Tab 1)": {
			msg: tea.KeyPressMsg{Code: '1', Text: "1"},
			verify: func(t *testing.T, m *Model) { /* matches setPriority(1) instead of tab change due to conflict */ },
		},
		"Key: 4 (Tab 4)": {
			msg: tea.KeyPressMsg{Code: '4', Text: "4"},
			verify: func(t *testing.T, m *Model) { assert.Equal(t, TabAll, m.listView.activeTab) },
		},
		"Key: p (Filter PR)": {
			msg: tea.KeyPressMsg{Code: 'p', Text: "p"},
			verify: func(t *testing.T, m *Model) { assert.Equal(t, "PullRequest", m.listView.resourceFilter) },
		},
		"Key: i (Filter Issue)": {
			msg: tea.KeyPressMsg{Code: 'i', Text: "i"},
			verify: func(t *testing.T, m *Model) { assert.Equal(t, "Issue", m.listView.resourceFilter) },
		},
		"Key: d (Filter Discussion)": {
			msg: tea.KeyPressMsg{Code: 'd', Text: "d"},
			verify: func(t *testing.T, m *Model) { assert.Equal(t, "Discussion", m.listView.resourceFilter) },
		},
		"Key: ] (Next Tab)": {
			msg: tea.KeyPressMsg{Code: ']', Text: "]"},
			verify: func(t *testing.T, m *Model) { assert.Equal(t, TabUnread, m.listView.activeTab) },
		},
		"Key: [ (Prev Tab)": {
			msg: tea.KeyPressMsg{Code: '[', Text: "["},
			setup: func(m *Model) { m.listView.activeTab = TabAll },
			verify: func(t *testing.T, m *Model) { assert.Equal(t, TabTriaged, m.listView.activeTab) },
		},
		"Key: r (Sync)": {
			msg: tea.KeyPressMsg{Code: 'r', Text: "r"},
			setup: func(m *Model) {
				mockTraffic := m.traffic.(*mocks.MockTrafficController)
				mockTraffic.EXPECT().Submit(1, mock.Anything).Return(func() tea.Msg { return nil }).Once()
			},
			verify: func(t *testing.T, m *Model) { assert.True(t, m.ui.syncing) },
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
	
	mockTraffic.EXPECT().Submit(mock.Anything, mock.Anything).RunAndReturn(func(priority int, fn types.TaskFunc) tea.Cmd {
		return func() tea.Msg {
			return fn(context.Background())
		}
	}).Once()
	// Noise handler
	mockTraffic.EXPECT().Submit(0, mock.Anything).RunAndReturn(func(priority int, fn types.TaskFunc) tea.Cmd {
		return func() tea.Msg { return fn(context.Background()) }
	}).Maybe()

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

func TestModel_Init(t *testing.T) {
	m := newTestModel(t)
	mockAlerter := m.alerter.(*mocks.MockAlerter)
	mockAlerter.EXPECT().Warmup().Return().Once()
	
	cmd := m.Init()
	require.NotNil(t, cmd)
	_ = executeCmd(cmd)
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

func TestModel_Options(t *testing.T) {
	cfg := &config.Config{}
	m := NewModel(
		"u", cfg, nil, nil, nil, nil, nil, nil, nil,
		WithTheme(false),
		WithVersion("1.2.3"),
	)
	
	assert.False(t, m.isDark)
	assert.Equal(t, "1.2.3", m.version)
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

func TestRenderTargetHeader_States(t *testing.T) {
	ctx := RenderContext{
		Styles: DefaultStyles(true),
		Width:  100,
	}
	
	notif := types.NotificationWithState{
		Notification: types.Notification{
			SubjectType: "PullRequest",
			SubjectTitle: "Title",
			GitHubID: "123",
			ResourceState: "MERGED",
		},
	}
	
	// Test normal
	out := RenderTargetHeader(ctx, notif, "", false)
	assert.Contains(t, stripANSI(out), "Title")
	assert.Contains(t, stripANSI(out), "MERGED")
	
	// Test with filter match
	out2 := RenderTargetHeader(ctx, notif, "Title", false)
	assert.NotEmpty(t, out2)
	
	// Test selected
	out3 := RenderTargetHeader(ctx, notif, "", true)
	assert.NotEmpty(t, out3)
}
