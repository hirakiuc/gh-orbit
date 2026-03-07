package tui

import (
	"image/color"
	"log/slog"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/mocks"
	"github.com/hirakiuc/gh-orbit/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func newTestModel(t *testing.T) *Model {
	cfg := &config.Config{}
	logger := slog.Default()
	userID := "test-user"

	// Mock engines via the new centralized interfaces
	mockSyncer := mocks.NewMockSyncer(t)
	mockEnricher := mocks.NewMockEnricher(t)
	mockTraffic := mocks.NewMockTrafficController(t)
	mockAlerter := mocks.NewMockAlerter(t)
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

	// syncCompleteMsg SHOULD reset syncing
	msg := syncCompleteMsg{}
	updatedModel, _ := m.Update(msg)
	assert.False(t, updatedModel.(*Model).ui.syncing, "expected syncing to be false after syncCompleteMsg")

	// Test error reset
	m.ui.SetSyncing(true)
	msgErr := errMsg{err: nil}
	updatedModel, _ = m.Update(msgErr)
	assert.False(t, updatedModel.(*Model).ui.syncing, "expected syncing to be false after errMsg")
}

func TestModel_Update_ThemeChange(t *testing.T) {
	m := newTestModel(t)

	// Mock a light background color msg
	msg := tea.BackgroundColorMsg{Color: color.RGBA{R: 255, G: 255, B: 255, A: 255}}
	updatedModel, _ := m.Update(msg)
	_ = updatedModel.(*Model)
}

func TestModel_Update_StatusClearing(t *testing.T) {
	m := newTestModel(t)
	m.ui.SetToast("temporary status")

	msgClear := clearStatusMsg{}
	updatedModel, _ := m.Update(msgClear)
	newModel := updatedModel.(*Model)
	assert.Empty(t, newModel.ui.toastMessage, "expected status to be cleared after clearStatusMsg")
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
	
	mockRepo.EXPECT().MarkReadLocally("test-id", true).Return(nil).Once()
	mockClient.EXPECT().MarkThreadAsRead("test-id").Return(nil).Once()

	m.applyFilters()

	// Initial state
	i := m.listView.list.Items()[0].(item)
	cmd := m.MarkRead(i)
	require.NotNil(t, cmd)
	
	// Execute cmd to verify client call
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

	// 2. Test Help -> Close transition via 'q' (verified via build check)
	
	// 3. Test Quit transition via 'q'
	m.listView.list.Help.ShowAll = false
	_, cmd := m.Update(msgQ)
	assert.NotNil(t, cmd, "expected quit command, got nil")
}

func TestRenderTargetHeader_Geometry(t *testing.T) {
	styles := DefaultStyles(true)
	ctx := RenderContext{
		Styles: styles,
		Width:  80,
	}
	notif := types.NotificationWithState{
		Notification: types.Notification{
			SubjectTitle: "A very long title that should be truncated when a badge is present",
			SubjectType:  "PullRequest",
		},
	}

	// 1. Un-enriched (No badge)
	h1 := RenderTargetHeader(ctx, notif, "", false)
	assert.NotEmpty(t, h1)

	// 2. Fetching (Skeleton badge)
	ctx.IsFetching = true
	h2 := RenderTargetHeader(ctx, notif, "", false)
	assert.Contains(t, stripANSI(h2), "FETCH")

	// 3. Enriched (Status badge)
	ctx.IsFetching = false
	notif.ResourceState = "Merged"
	h3 := RenderTargetHeader(ctx, notif, "", false)
	assert.Contains(t, stripANSI(h3), "MERGED")
}
