package api

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/db"
	"github.com/hirakiuc/gh-orbit/internal/mocks"
	"github.com/hirakiuc/gh-orbit/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestAlertService_Throttling(t *testing.T) {
	logger := slog.Default()
	ctx := context.Background()
	
	// We use real in-memory DB for the Repository part of the Hybrid strategy
	database, err := db.OpenInMemory(ctx, logger)
	require.NoError(t, err)
	defer func() { _ = database.Close() }()

	cfg := &config.Config{}
	cfg.Notifications.Enabled = true

	t.Run("Silent Initial Baseline", func(t *testing.T) {
		mockNative := mocks.NewMockNotifier(t)
		
		service := NewAlertService(ctx, cfg, database, logger)
		service.native = mockNative // Inject mock

		// 1. Initial state (empty DB)
		service.SyncStart(ctx)
		assert.True(t, service.isInitializing)

		err := service.Notify(ctx, types.GHNotification{
			ID: "1",
			Repository: struct {
				FullName string `json:"full_name"`
			}{FullName: "repo/a"},
			Subject: struct {
				Title  string `json:"title"`
				URL    string `json:"url"`
				Type   string `json:"type"`
				NodeID string `json:"node_id"`
			}{Title: "T1"},
		})
		require.NoError(t, err)
		// mockNative.Notify should NOT be called
	})

	t.Run("Throttling Logic", func(t *testing.T) {
		mockNative := mocks.NewMockNotifier(t)
		
		// Seed DB to end baseline
		err := database.UpsertNotification(db.Notification{GitHubID: "seed", UpdatedAt: time.Now()})
		require.NoError(t, err)

		service := NewAlertService(ctx, cfg, database, logger)
		service.native = mockNative

		mockNative.EXPECT().Status().Return(types.StatusHealthy).Maybe()
		
		// Expect 5 individual alerts
		mockNative.EXPECT().Notify(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Times(5)
		// Expect 1 summary alert
		mockNative.EXPECT().Notify(mock.Anything, "New Notifications", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()

		service.SyncStart(ctx)
		assert.False(t, service.isInitializing)

		// Send 10 notifications
		for i := 1; i <= 10; i++ {
			_ = service.Notify(ctx, types.GHNotification{
				ID: "id",
				Repository: struct {
					FullName string `json:"full_name"`
				}{FullName: "repo/a"},
				Subject: struct {
					Title  string `json:"title"`
					URL    string `json:"url"`
					Type   string `json:"type"`
					NodeID string `json:"node_id"`
				}{Title: "Notification"},
			})
		}
	})
}
