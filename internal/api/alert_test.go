package api

import (
	"context"
	"log/slog"
	"testing"

	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/mocks"
	"github.com/hirakiuc/gh-orbit/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestAlertService_Notify(t *testing.T) {
	ctx := context.Background()
	logger := slog.Default()
	cfg := &config.Config{
		Notifications: config.NotificationsConfig{
			Enabled: true,
		},
	}
	
	mockRepo := mocks.NewMockAlertRepository(t)
	mockNative := mocks.NewMockNotifier(t)
	mockFallback := mocks.NewMockNotifier(t)
	mockExecutor := mocks.NewMockCommandExecutor(t)
	mockExecutor.EXPECT().Execute(mock.Anything, "sysctl", mock.Anything, mock.Anything).Return([]byte("kern.osversion: 24G517"), nil).Maybe()

	s := NewAlertService(cfg, mockRepo, mockNative, mockFallback, mockExecutor, logger)
	
	n := types.GHNotification{
		ID: "1",
		Reason: "mention",
		Subject: struct {
			Title  string `json:"title"`
			URL    string `json:"url"`
			Type   string `json:"type"`
			NodeID string `json:"node_id"`
		}{Title: "Mention", URL: "url", Type: "Issue"},
	}

	// 1. Initial Sync: Empty DB -> isInitializing = true
	mockRepo.EXPECT().ListNotifications(mock.Anything).Return([]types.NotificationWithState{}, nil).Once()
	s.SyncStart(ctx)

	// 2. Notify during first sync: Always silent
	// Importance is calculated, but no notifier call
	err := s.Notify(ctx, n)
	require.NoError(t, err)

	// 3. Second Sync: DB not empty -> isInitializing = false
	mockRepo.EXPECT().ListNotifications(mock.Anything).Return([]types.NotificationWithState{
		{Notification: types.Notification{GitHubID: "1"}},
	}, nil).Once()
	s.SyncStart(ctx)

	// 4. Notify during second sync: Should trigger notifier
	n2 := n
	n2.ID = "2"
	
	mockNative.EXPECT().Status().Return(StatusHealthy).Maybe()
	mockNative.EXPECT().Notify(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil).Once()
	
	err = s.Notify(ctx, n2)
	require.NoError(t, err)
}

func TestAlertService_SyncStart(t *testing.T) {
	mockRepo := mocks.NewMockAlertRepository(t)
	mockRepo.EXPECT().ListNotifications(mock.Anything).Return([]types.NotificationWithState{}, nil).Maybe()
	
	mockNative := mocks.NewMockNotifier(t)
	mockFallback := mocks.NewMockNotifier(t)
	mockExecutor := mocks.NewMockCommandExecutor(t)
	s := NewAlertService(&config.Config{}, mockRepo, mockNative, mockFallback, mockExecutor, slog.Default())

	s.SyncStart(context.Background())
}

func TestAlertService_Metadata(t *testing.T) {
	mockRepo := mocks.NewMockAlertRepository(t)
	mockNative := mocks.NewMockNotifier(t)
	mockFallback := mocks.NewMockNotifier(t)
	mockExecutor := mocks.NewMockCommandExecutor(t)
	mockExecutor.EXPECT().Execute(mock.Anything, "sysctl", mock.Anything, mock.Anything).Return([]byte("kern.osversion: 24G517"), nil).Maybe()
	s := NewAlertService(&config.Config{}, mockRepo, mockNative, mockFallback, mockExecutor, slog.Default())

	// 1. ActiveTierInfo
	mockNative.EXPECT().Status().Return(StatusHealthy).Maybe()
	mockFallback.EXPECT().Status().Return(StatusHealthy).Maybe()
	tier, status := s.ActiveTierInfo()
	assert.NotEmpty(t, tier)
	assert.Equal(t, StatusHealthy, status)

	// 2. BridgeStatus
	assert.Equal(t, StatusHealthy, s.BridgeStatus())

	// 3. Shutdown
	mockNative.EXPECT().Shutdown(mock.Anything).Return().Maybe()
	mockFallback.EXPECT().Shutdown(mock.Anything).Return().Maybe()
	s.Shutdown(context.Background())
}

func TestAlertService_Throttling(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{Notifications: config.NotificationsConfig{Enabled: true}}
	mockRepo := mocks.NewMockAlertRepository(t)
	mockNative := mocks.NewMockNotifier(t)
	mockExecutor := mocks.NewMockCommandExecutor(t)
	s := NewAlertService(cfg, mockRepo, mockNative, nil, mockExecutor, slog.Default())

	// Move out of initialization
	mockRepo.EXPECT().ListNotifications(mock.Anything).Return([]types.NotificationWithState{{}}, nil).Once()
	s.SyncStart(ctx)

	n := types.GHNotification{ID: "1", Reason: "mention"}
	
	// Mock Status for getNotifier
	mockNative.EXPECT().Status().Return(StatusHealthy).Maybe()

	// 1-5: Individual alerts
	mockNative.EXPECT().Notify(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil).Times(5)
	
	for i := 0; i < 5; i++ {
		err := s.Notify(ctx, n)
		require.NoError(t, err)
	}

	// 6: Summary alert
	mockNative.EXPECT().Notify(mock.Anything, "New Notifications", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil).Once()
	err := s.Notify(ctx, n)
	require.NoError(t, err)

	// 7+: Silent
	err = s.Notify(ctx, n)
	require.NoError(t, err)
}

func TestAlertService_SpecificReasons(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{
		Notifications: config.NotificationsConfig{
			Enabled: true,
			Reasons: []string{"mention"},
		},
	}
	mockRepo := mocks.NewMockAlertRepository(t)
	mockNative := mocks.NewMockNotifier(t)
	mockExecutor := mocks.NewMockCommandExecutor(t)
	s := NewAlertService(cfg, mockRepo, mockNative, nil, mockExecutor, slog.Default())

	mockRepo.EXPECT().ListNotifications(mock.Anything).Return([]types.NotificationWithState{{}}, nil).Once()
	s.SyncStart(ctx)

	mockNative.EXPECT().Status().Return(StatusHealthy).Maybe()

	// 1. Matched reason: notify
	n1 := types.GHNotification{ID: "1", Reason: "mention"}
	mockNative.EXPECT().Notify(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil).Once()
	require.NoError(t, s.Notify(ctx, n1))

	// 2. Unmatched reason: skip
	n2 := types.GHNotification{ID: "2", Reason: "other"}
	require.NoError(t, s.Notify(ctx, n2))
}
