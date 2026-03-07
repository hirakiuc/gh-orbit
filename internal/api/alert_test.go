package api

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/db"
	"github.com/hirakiuc/gh-orbit/internal/mocks"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestAlertService_Throttling(t *testing.T) {
	cfg := &config.Config{
		Notifications: config.NotificationsConfig{
			Enabled: true,
		},
	}
	logger := slog.Default()
	ctx := context.Background()

	t.Run("Silent Initial Baseline", func(t *testing.T) {
		mockRepo := mocks.NewMockAlertRepository(t)
		// Empty DB means isInitializing will be true
		mockRepo.EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Once()

		service := NewAlertService(ctx, cfg, mockRepo, logger)
		service.SyncStart(ctx)

		err := service.Notify(ctx, GHNotification{ID: "1"})
		require.NoError(t, err)
		// No notifier calls should happen because isInitializing is true
	})

	t.Run("Throttling Logic", func(t *testing.T) {
		// 1. Setup real in-memory DB to handle ListNotifications logic
		database, err := db.OpenInMemory(ctx, logger)
		require.NoError(t, err)
		defer func() { _ = database.Close() }()

		// Pre-populate one notification so isInitializing becomes false
		err = database.UpsertNotification(ctx, db.Notification{GitHubID: "0", UpdatedAt: time.Now()})
		require.NoError(t, err)

		mockNative := mocks.NewMockNotifier(t)
		// Expect Status() calls during tiered notifier selection
		mockNative.EXPECT().Status().Return(StatusHealthy).Maybe()

		service := NewAlertService(ctx, cfg, database, logger)
		service.native = mockNative

		service.SyncStart(ctx)

		// Expect 5 individual alerts
		mockNative.EXPECT().Notify(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Times(5)
		// Expect 1 summary alert on the 6th notification
		mockNative.EXPECT().Notify(mock.Anything, "New Notifications", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()

		for i := 1; i <= 10; i++ {
			_ = service.Notify(ctx, GHNotification{
				ID:     "id",
				Reason: "mention",
				Repository: struct {
					FullName string `json:"full_name"`
				}{FullName: "owner/repo"},
				Subject: struct {
					Title  string `json:"title"`
					URL    string `json:"url"`
					Type   string `json:"type"`
					NodeID string `json:"node_id"`
				}{Title: "PR Title"},
			})
		}
	})
}
