package api

import (
	"context"
	"log/slog"
	"testing"

	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/github"
	"github.com/hirakiuc/gh-orbit/internal/mocks"
	"github.com/hirakiuc/gh-orbit/internal/triage"
	"github.com/hirakiuc/gh-orbit/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestAlertService_Notify(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{Notifications: config.NotificationsConfig{Enabled: true}}
	mockRepo := mocks.NewMockAlertRepository(t)
	mockNative := mocks.NewMockNotifier(t)

	s := NewAlertService(cfg, mockRepo, mockNative, nil, nil, slog.Default())

	t.Run("Standard Alert", func(t *testing.T) {
		n := github.Notification{
			Reason: "mention",
			Repository: struct {
				FullName string `json:"full_name"`
			}{FullName: "owner/repo"},
			Subject: struct {
				Title  string `json:"title"`
				URL    string `json:"url"`
				Type   string `json:"type"`
				NodeID string `json:"node_id"`
			}{Title: "Issue Title", URL: "url"},
		}

		mockNative.EXPECT().Status().Return(types.StatusHealthy).Maybe()
		mockNative.EXPECT().Notify(mock.Anything, "owner/repo", "Issue Title", "mention", "url", 3).Return(nil).Once()

		err := s.Notify(ctx, n)
		assert.NoError(t, err)
	})

	t.Run("Disabled Notifications", func(t *testing.T) {
		s.config.Notifications.Enabled = false
		err := s.Notify(ctx, github.Notification{})
		assert.NoError(t, err)
	})
}

func TestAlertService_SyncStart(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockAlertRepository(t)
	s := NewAlertService(&config.Config{}, mockRepo, nil, nil, nil, slog.Default())

	t.Run("Initial Baseline Detection", func(t *testing.T) {
		mockRepo.EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Once()
		s.SyncStart(ctx)
		assert.True(t, s.isInitializing)
	})

	t.Run("Existing Data detection", func(t *testing.T) {
		mockRepo.EXPECT().ListNotifications(mock.Anything).Return([]triage.NotificationWithState{{}}, nil).Once()
		s.SyncStart(ctx)
		assert.False(t, s.isInitializing)
	})
}

func TestAlertService_Metadata(t *testing.T) {
	mockNative := mocks.NewMockNotifier(t)
	mockFallback := mocks.NewMockNotifier(t)
	s := NewAlertService(&config.Config{}, nil, mockNative, mockFallback, nil, slog.Default())

	// 1. ActiveTierInfo
	mockNative.EXPECT().Status().Return(types.StatusHealthy).Once()
	tier, status := s.ActiveTierInfo()
	assert.NotEmpty(t, tier)
	assert.Equal(t, types.StatusHealthy, status)

	// 2. BridgeStatus
	mockNative.EXPECT().Status().Return(types.StatusHealthy).Once()
	assert.Equal(t, types.StatusHealthy, s.BridgeStatus())

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

	t.Run("Throttle limit reached", func(t *testing.T) {
		s.syncAlertCount = 5
		mockNative.EXPECT().Status().Return(types.StatusHealthy).Maybe()
		mockNative.EXPECT().Notify(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()

		err := s.Notify(ctx, github.Notification{Reason: "mention"})
		assert.NoError(t, err)
		assert.Equal(t, 6, s.syncAlertCount)

		// Subsequent calls should be ignored
		err = s.Notify(ctx, github.Notification{Reason: "mention"})
		assert.NoError(t, err)
		assert.Equal(t, 6, s.syncAlertCount)
	})
}

func TestAlertService_RefreshBridgeHealth(t *testing.T) {
	ctx := context.Background()
	mockRepo := mocks.NewMockAlertRepository(t)
	mockExecutor := mocks.NewMockCommandExecutor(t)
	mockNative := mocks.NewMockNotifier(t)
	s := NewAlertService(&config.Config{}, mockRepo, mockNative, nil, mockExecutor, slog.Default())

	t.Run("Successful Refresh", func(t *testing.T) {
		mockRepo.EXPECT().UpdateBridgeHealth(mock.Anything, mock.Anything).Return(nil).Twice()
		mockExecutor.EXPECT().Execute(mock.Anything, "sw_vers", "-productVersion").Return([]byte("14.4"), nil).Once()
		mockNative.EXPECT().Status().Return(types.StatusHealthy).Maybe()

		status, err := s.RefreshBridgeHealth(ctx)
		assert.NoError(t, err)
		assert.Equal(t, types.StatusHealthy, status)
	})
}
