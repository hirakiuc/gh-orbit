package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/github"
	"github.com/hirakiuc/gh-orbit/internal/mocks"
	"github.com/hirakiuc/gh-orbit/internal/models"
	"github.com/hirakiuc/gh-orbit/internal/triage"
	"github.com/hirakiuc/gh-orbit/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestSyncEngine_Sync(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"
	logger := slog.Default()

	t.Run("Full Sync Flow", func(t *testing.T) {
		mockFetcher := mocks.NewMockFetcher(t)
		mockRepo := mocks.NewMockSyncRepository(t)
		mockAlerter := mocks.NewMockAlerter(t)

		meta := &models.SyncMeta{
			UserID: userID,
			Key:    "notifications",
		}

		notifs := []github.Notification{
			{ID: "1", UpdatedAt: time.Now()},
		}

		mockRepo.EXPECT().GetSyncMeta(mock.Anything, userID, "notifications").Return(meta, nil).Once()
		mockAlerter.EXPECT().SyncStart(mock.Anything).Return().Once()
		mockFetcher.EXPECT().FetchNotifications(mock.Anything, meta, false).Return(notifs, meta, models.RateLimitInfo{}, nil).Once()
		mockRepo.EXPECT().UpsertNotifications(mock.Anything, mock.Anything).Return(nil).Once()
		mockRepo.EXPECT().GetNotification(mock.Anything, "1").Return(&triage.NotificationWithState{
			State: triage.State{IsNotified: false},
		}, nil).Once()

		// Expect Notify because UpdatedAt is After LastSyncAt (which is zero)
		mockAlerter.EXPECT().Notify(mock.Anything, mock.Anything).Return(nil).Once()

		mockRepo.EXPECT().MarkNotifiedBatch(mock.Anything, []string{"1"}).Return(nil).Once()
		mockRepo.EXPECT().UpdateSyncMeta(mock.Anything, mock.Anything).Return(nil).Once()

		engine, err := NewSyncEngine(SyncParams{
			Fetcher: mockFetcher,
			DB:      mockRepo,
			Alerts:  mockAlerter,
			Logger:  logger,
		})
		assert.NoError(t, err)
		_, err = engine.Sync(ctx, userID, false)

		require.NoError(t, err)
	})

	t.Run("Skips Sync When Interval Not Reached", func(t *testing.T) {
		mockFetcher := mocks.NewMockFetcher(t)
		mockRepo := mocks.NewMockSyncRepository(t)

		recentMeta := &models.SyncMeta{
			UserID:       userID,
			Key:          "notifications",
			PollInterval: 60,
			LastSyncAt:   time.Now(),
		}

		mockRepo.EXPECT().GetSyncMeta(mock.Anything, userID, "notifications").Return(recentMeta, nil).Once()

		engine, err := NewSyncEngine(SyncParams{
			Fetcher: mockFetcher,
			DB:      mockRepo,
			Logger:  logger,
		})
		assert.NoError(t, err)
		_, err = engine.Sync(ctx, userID, false)

		assert.ErrorIs(t, err, types.ErrSyncIntervalNotReached)
	})

	t.Run("Forces Sync Even if Interval Not Reached", func(t *testing.T) {
		mockFetcher := mocks.NewMockFetcher(t)
		mockRepo := mocks.NewMockSyncRepository(t)

		recentMeta := &models.SyncMeta{
			UserID:       userID,
			Key:          "notifications",
			PollInterval: 60,
			LastSyncAt:   time.Now(),
		}

		mockRepo.EXPECT().GetSyncMeta(mock.Anything, userID, "notifications").Return(recentMeta, nil).Once()
		mockFetcher.EXPECT().FetchNotifications(mock.Anything, recentMeta, true).Return(nil, recentMeta, models.RateLimitInfo{}, nil).Once()
		mockRepo.EXPECT().UpdateSyncMeta(mock.Anything, mock.Anything).Return(nil).Once()

		engine, err := NewSyncEngine(SyncParams{
			Fetcher: mockFetcher,
			DB:      mockRepo,
			Logger:  logger,
		})
		assert.NoError(t, err)
		_, err = engine.Sync(ctx, userID, true)

		require.NoError(t, err)
	})

	t.Run("Chaos Paths - API Errors", func(t *testing.T) {
		tests := []struct {
			name     string
			apiErr   error
			expected error
		}{
			{"Unauthorized", types.ErrUnauthorized, types.ErrUnauthorized},
			{"Internal Error", types.ErrInternalServerError, types.ErrInternalServerError},
			{"Rate Limited", &models.RateLimitError{RetryAfter: time.Minute}, &models.RateLimitError{}},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				mockFetcher := mocks.NewMockFetcher(t)
				mockRepo := mocks.NewMockSyncRepository(t)
				mockRepo.EXPECT().GetSyncMeta(mock.Anything, userID, "notifications").Return(&models.SyncMeta{}, nil).Once()
				mockFetcher.EXPECT().FetchNotifications(mock.Anything, mock.Anything, false).Return(nil, nil, models.RateLimitInfo{}, tt.apiErr).Once()
				mockRepo.EXPECT().UpdateSyncMeta(mock.Anything, mock.Anything).Return(nil).Once()

				engine, _ := NewSyncEngine(SyncParams{Fetcher: mockFetcher, DB: mockRepo, Logger: logger})
				_, err := engine.Sync(ctx, userID, false)

				if tt.name == "Rate Limited" {
					assert.ErrorAs(t, err, &tt.expected)
				} else {
					assert.ErrorIs(t, err, tt.expected)
				}
			})
		}
	})
}

func TestNewSyncEngine_Guards(t *testing.T) {
	mockFetcher := mocks.NewMockFetcher(t)
	mockRepo := mocks.NewMockSyncRepository(t)
	logger := slog.Default()

	t.Run("Missing Fetcher", func(t *testing.T) {
		_, err := NewSyncEngine(SyncParams{DB: mockRepo, Logger: logger})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "fetcher is required")
	})

	t.Run("Missing DB", func(t *testing.T) {
		_, err := NewSyncEngine(SyncParams{Fetcher: mockFetcher, Logger: logger})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database is required")
	})

	t.Run("Missing Logger", func(t *testing.T) {
		_, err := NewSyncEngine(SyncParams{Fetcher: mockFetcher, DB: mockRepo})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "logger is required")
	})
}

func TestConditionalRequest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == "etag-123" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", "etag-123")
		_ = json.NewEncoder(w).Encode([]github.Notification{})
	}))
	defer ts.Close()

	client := github.NewTestClient(ts.Client(), ts.URL+"/")
	fetcher := github.NewNotificationFetcher(client, slog.Default())
	meta := &models.SyncMeta{ETag: "etag-123"}

	_, _, _, err := fetcher.FetchNotifications(context.Background(), meta, false)
	require.NoError(t, err)
}

func TestETagSanitization(t *testing.T) {
	t.Run("Fetcher Ignores Invalid ETags", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("ETag", `W/""`) // Corrupted header
			_ = json.NewEncoder(w).Encode([]github.Notification{})
		}))
		defer ts.Close()

		client := github.NewTestClient(ts.Client(), ts.URL+"/")
		fetcher := github.NewNotificationFetcher(client, slog.Default())

		_, newMeta, _, err := fetcher.FetchNotifications(context.Background(), &models.SyncMeta{}, false)
		require.NoError(t, err)
		assert.Empty(t, newMeta.ETag)
	})
}

func TestPagination(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/notifications" {
			w.Header().Set("Link", fmt.Sprintf(`<%s/page2>; rel="next"`, "http://"+r.Host))
			_ = json.NewEncoder(w).Encode([]github.Notification{{ID: "1"}, {ID: "2"}})
		} else {
			_ = json.NewEncoder(w).Encode([]github.Notification{{ID: "3"}})
		}
	}))
	defer ts.Close()

	client := github.NewTestClient(ts.Client(), ts.URL+"/")
	fetcher := github.NewNotificationFetcher(client, slog.Default())

	notifs, _, _, err := fetcher.FetchNotifications(context.Background(), &models.SyncMeta{}, false)
	require.NoError(t, err)
	assert.Len(t, notifs, 3)
}

func TestFetcher_ErrorHandling(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer ts.Close()

	client := github.NewTestClient(ts.Client(), ts.URL+"/")
	fetcher := github.NewNotificationFetcher(client, slog.Default())

	_, _, _, err := fetcher.FetchNotifications(context.Background(), &models.SyncMeta{}, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}
