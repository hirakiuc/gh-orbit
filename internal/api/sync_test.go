package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newHTTPClient(t *testing.T, fn roundTripFunc) *http.Client {
	t.Helper()

	return &http.Client{Transport: fn}
}

func newJSONResponse(status int, body string, headers http.Header) *http.Response {
	if headers == nil {
		headers = make(http.Header)
	}

	return &http.Response{
		StatusCode: status,
		Header:     headers,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

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

func TestSyncEngine_InitSyncMeta(t *testing.T) {
	ctx := context.Background()
	userID := "user-1"
	key := "notifications"
	logger := slog.Default()

	t.Run("First Run - No Meta in DB", func(t *testing.T) {
		mockRepo := mocks.NewMockSyncRepository(t)
		mockRepo.EXPECT().GetSyncMeta(ctx, userID, key).Return(nil, nil).Once()

		engine := &SyncEngine{db: mockRepo, logger: logger}
		meta, err := engine.initSyncMeta(ctx, userID, key, 123)

		assert.NoError(t, err)
		assert.NotNil(t, meta)
		assert.Equal(t, userID, meta.UserID)
		assert.Equal(t, DefaultPollInterval, meta.PollInterval)
	})

	t.Run("Self-Healing - Corrupted ETag", func(t *testing.T) {
		mockRepo := mocks.NewMockSyncRepository(t)
		corruptedMeta := &models.SyncMeta{
			UserID: userID,
			Key:    key,
			ETag:   `W/""`,
		}
		mockRepo.EXPECT().GetSyncMeta(ctx, userID, key).Return(corruptedMeta, nil).Once()

		engine := &SyncEngine{db: mockRepo, logger: logger}
		meta, err := engine.initSyncMeta(ctx, userID, key, 123)

		assert.NoError(t, err)
		assert.Empty(t, meta.ETag, "Corrupted ETag should be cleared")
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
	client := github.NewTestClient(newHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("If-None-Match") == "etag-123" {
			return newJSONResponse(http.StatusNotModified, "", nil), nil
		}
		headers := make(http.Header)
		headers.Set("ETag", "etag-123")

		body, err := json.Marshal([]github.Notification{})
		require.NoError(t, err)
		return newJSONResponse(http.StatusOK, string(body), headers), nil
	}), "https://api.test/")
	fetcher := github.NewNotificationFetcher(client, slog.Default())
	meta := &models.SyncMeta{ETag: "etag-123"}

	_, _, _, err := fetcher.FetchNotifications(context.Background(), meta, false)
	require.NoError(t, err)
}

func TestETagSanitization(t *testing.T) {
	t.Run("Fetcher Ignores Invalid ETags", func(t *testing.T) {
		client := github.NewTestClient(newHTTPClient(t, func(r *http.Request) (*http.Response, error) {
			headers := make(http.Header)
			headers.Set("ETag", `W/""`) // Corrupted header

			body, err := json.Marshal([]github.Notification{})
			require.NoError(t, err)
			return newJSONResponse(http.StatusOK, string(body), headers), nil
		}), "https://api.test/")
		fetcher := github.NewNotificationFetcher(client, slog.Default())

		_, newMeta, _, err := fetcher.FetchNotifications(context.Background(), &models.SyncMeta{}, false)
		require.NoError(t, err)
		assert.Empty(t, newMeta.ETag)
	})
}

func TestPagination(t *testing.T) {
	client := github.NewTestClient(newHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/notifications" {
			headers := make(http.Header)
			headers.Set("Link", `<https://api.test/page2>; rel="next"`)
			body, err := json.Marshal([]github.Notification{{ID: "1"}, {ID: "2"}})
			require.NoError(t, err)
			return newJSONResponse(http.StatusOK, string(body), headers), nil
		}

		assert.Equal(t, "/page2", r.URL.Path)
		body, err := json.Marshal([]github.Notification{{ID: "3"}})
		require.NoError(t, err)
		return newJSONResponse(http.StatusOK, string(body), nil), nil
	}), "https://api.test/")
	fetcher := github.NewNotificationFetcher(client, slog.Default())

	notifs, _, _, err := fetcher.FetchNotifications(context.Background(), &models.SyncMeta{}, false)
	require.NoError(t, err)
	assert.Len(t, notifs, 3)
}

func TestFetcher_ErrorHandling(t *testing.T) {
	client := github.NewTestClient(newHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		return newJSONResponse(http.StatusUnauthorized, "", nil), nil
	}), "https://api.test/")
	fetcher := github.NewNotificationFetcher(client, slog.Default())

	_, _, _, err := fetcher.FetchNotifications(context.Background(), &models.SyncMeta{}, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}
