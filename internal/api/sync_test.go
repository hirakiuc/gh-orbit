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

	"github.com/hirakiuc/gh-orbit/internal/mocks"
	"github.com/hirakiuc/gh-orbit/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestParseLinkHeader(t *testing.T) {
	header := `<https://api.github.com/resource?page=2>; rel="next", <https://api.github.com/resource?page=5>; rel="last"`
	links := parseLinkHeader(header)

	assert.Equal(t, "https://api.github.com/resource?page=2", links["next"])
	assert.Equal(t, "https://api.github.com/resource?page=5", links["last"])
}

func TestParseLinkHeader_EdgeCases(t *testing.T) {
	tests := map[string]struct {
		input string
		want  map[string]string
	}{
		"Empty": {input: "", want: map[string]string{}},
		"Malformed": {input: "not a link", want: map[string]string{}},
		"Missing Rel": {input: "<http://url>; foo=\"bar\"", want: map[string]string{}},
		"Multiple": {
			input: "<http://url1>; rel=\"next\", <http://url2>; rel=\"last\"",
			want:  map[string]string{"next": "http://url1", "last": "http://url2"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := parseLinkHeader(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestSyncEngine_Sync(t *testing.T) {
	ctx := context.Background()
	logger := slog.Default()
	userID := "user-1"

	t.Run("Successful Initial Sync", func(t *testing.T) {
		mockFetcher := mocks.NewMockFetcher(t)
		mockRepo := mocks.NewMockSyncRepository(t)

		initialMeta := &types.SyncMeta{UserID: userID, Key: "notifications", PollInterval: 60}
		notifs := []types.GHNotification{
			{ID: "1", UpdatedAt: time.Now()},
		}

		// Expectations
		mockRepo.EXPECT().GetSyncMeta(mock.Anything, userID, "notifications").Return(nil, nil).Once()
		mockFetcher.EXPECT().FetchNotifications(mock.Anything, initialMeta, true).Return(notifs, initialMeta, 5000, nil).Once()
		mockRepo.EXPECT().UpsertNotification(mock.Anything, mock.Anything).Return(nil).Once()
		mockRepo.EXPECT().GetNotification(mock.Anything, "1").Return(&types.NotificationWithState{}, nil).Once()
		mockRepo.EXPECT().MarkNotifiedBatch(mock.Anything, []string{"1"}).Return(nil).Once()
		mockRepo.EXPECT().UpdateSyncMeta(mock.Anything, mock.Anything).Return(nil).Once()

		engine := NewSyncEngine(mockFetcher, mockRepo, nil, logger)
		remaining, err := engine.Sync(ctx, userID, true)

		require.NoError(t, err)
		assert.Equal(t, 5000, remaining)
	})

	t.Run("Skips Sync When Interval Not Reached", func(t *testing.T) {
		mockFetcher := mocks.NewMockFetcher(t)
		mockRepo := mocks.NewMockSyncRepository(t)

		recentMeta := &types.SyncMeta{
			UserID:       userID,
			Key:          "notifications",
			PollInterval: 60,
			LastSyncAt:   time.Now(),
		}

		mockRepo.EXPECT().GetSyncMeta(mock.Anything, userID, "notifications").Return(recentMeta, nil).Once()

		engine := NewSyncEngine(mockFetcher, mockRepo, nil, logger)
		_, err := engine.Sync(ctx, userID, false)

		require.NoError(t, err)
	})

	t.Run("Forces Sync Even if Interval Not Reached", func(t *testing.T) {
		mockFetcher := mocks.NewMockFetcher(t)
		mockRepo := mocks.NewMockSyncRepository(t)

		recentMeta := &types.SyncMeta{
			UserID:       userID,
			Key:          "notifications",
			PollInterval: 60,
			LastSyncAt:   time.Now(),
		}

		mockRepo.EXPECT().GetSyncMeta(mock.Anything, userID, "notifications").Return(recentMeta, nil).Once()
		mockFetcher.EXPECT().FetchNotifications(mock.Anything, recentMeta, true).Return(nil, recentMeta, 4999, nil).Once()
		mockRepo.EXPECT().UpdateSyncMeta(mock.Anything, mock.Anything).Return(nil).Once()

		engine := NewSyncEngine(mockFetcher, mockRepo, nil, logger)
		remaining, err := engine.Sync(ctx, userID, true)

		require.NoError(t, err)
		assert.Equal(t, 4999, remaining)
	})
}

func TestConditionalRequest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == "etag-123" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", "etag-123")
		_ = json.NewEncoder(w).Encode([]types.GHNotification{})
	}))
	defer ts.Close()

	client := &Client{
		http:    ts.Client(),
		baseURL: ts.URL + "/",
	}
	
	fetcher := NewNotificationFetcher(client, slog.Default())
	meta := &types.SyncMeta{ETag: "etag-123"}
	
	_, _, _, err := fetcher.FetchNotifications(context.Background(), meta, false)
	require.NoError(t, err)
}

func TestETagSanitization(t *testing.T) {
	t.Run("Fetcher Ignores Invalid ETags", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("ETag", `W/""`) // Corrupted header
			_ = json.NewEncoder(w).Encode([]types.GHNotification{})
		}))
		defer ts.Close()

		client := &Client{http: ts.Client(), baseURL: ts.URL + "/"}
		fetcher := NewNotificationFetcher(client, slog.Default())
		
		meta := &types.SyncMeta{ETag: "old-etag"}
		_, newMeta, _, _ := fetcher.FetchNotifications(context.Background(), meta, false)
		
		assert.NotEqual(t, `W/""`, newMeta.ETag)
		assert.Equal(t, "old-etag", newMeta.ETag)
	})

	t.Run("SyncEngine Self-Heals Corrupted ETag", func(t *testing.T) {
		ctx := context.Background()
		mockFetcher := mocks.NewMockFetcher(t)
		mockRepo := mocks.NewMockSyncRepository(t)

		corruptedMeta := &types.SyncMeta{
			UserID: "user-1",
			Key:    "notifications",
			ETag:   `W/""`,
		}

		// Initial expectation: returns corrupted meta
		mockRepo.EXPECT().GetSyncMeta(mock.Anything, "user-1", "notifications").Return(corruptedMeta, nil).Once()
		
		// The engine should clear the ETag before passing it to Fetcher
		healedMeta := *corruptedMeta
		healedMeta.ETag = ""
		
		mockFetcher.EXPECT().FetchNotifications(mock.Anything, &healedMeta, true).Return(nil, &healedMeta, 5000, nil).Once()
		mockRepo.EXPECT().UpdateSyncMeta(mock.Anything, mock.Anything).Return(nil).Once()

		engine := NewSyncEngine(mockFetcher, mockRepo, nil, slog.Default())
		_, err := engine.Sync(ctx, "user-1", true)
		
		require.NoError(t, err)
	})
}

func TestFetcher_Pagination(t *testing.T) {
	page1 := []types.GHNotification{{ID: "1"}, {ID: "2"}}
	page2 := []types.GHNotification{{ID: "3"}}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "" {
			// Construct relative next link to avoid closure issue with ts
			scheme := "http"
			if r.TLS != nil { scheme = "https" }
			w.Header().Set("Link", fmt.Sprintf("<%s://%s/notifications?page=2>; rel=\"next\"", scheme, r.Host))
			_ = json.NewEncoder(w).Encode(page1)
		} else {
			_ = json.NewEncoder(w).Encode(page2)
		}
	}))
	t.Cleanup(ts.Close)

	client := &Client{http: ts.Client(), baseURL: ts.URL + "/"}
	fetcher := NewNotificationFetcher(client, slog.Default())
	
	notifs, _, _, err := fetcher.FetchNotifications(context.Background(), &types.SyncMeta{}, true)
	require.NoError(t, err)
	assert.Len(t, notifs, 3)
}

func TestFetcher_APIErrors(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(ts.Close)

	client := &Client{http: ts.Client(), baseURL: ts.URL + "/"}
	fetcher := NewNotificationFetcher(client, slog.Default())
	
	_, _, _, err := fetcher.FetchNotifications(context.Background(), &types.SyncMeta{}, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}
