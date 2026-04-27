package github

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hirakiuc/gh-orbit/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNotificationFetcher_FetchNotifications(t *testing.T) {
	t.Run("Successful Fetch with Pagination", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/notifications" {
				w.Header().Set("Link", fmt.Sprintf(`<%s/page2>; rel="next"`, "http://"+r.Host))
				w.Header().Set("ETag", "etag-1")
				w.Header().Set("Last-Modified", "Mon, 02 Jan 2006 15:04:05 MST")
				w.Header().Set("X-Poll-Interval", "30")
				_ = json.NewEncoder(w).Encode([]Notification{{ID: "1"}, {ID: "2"}})
			} else {
				_ = json.NewEncoder(w).Encode([]Notification{{ID: "3"}})
			}
		}))
		defer ts.Close()

		client := NewTestClient(ts.Client(), ts.URL+"/")
		fetcher := NewNotificationFetcher(client, slog.Default())

		notifs, newMeta, rl, err := fetcher.FetchNotifications(context.Background(), &models.SyncMeta{}, false)
		require.NoError(t, err)
		assert.Len(t, notifs, 3)
		assert.Equal(t, "etag-1", newMeta.ETag)
		assert.Equal(t, 30, newMeta.PollInterval)
		assert.Equal(t, 5000, rl.Remaining)
	})

	t.Run("304 Not Modified", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "etag-old", r.Header.Get("If-None-Match"))
			w.WriteHeader(http.StatusNotModified)
		}))
		defer ts.Close()

		client := NewTestClient(ts.Client(), ts.URL+"/")
		fetcher := NewNotificationFetcher(client, slog.Default())
		meta := &models.SyncMeta{ETag: "etag-old"}

		notifs, newMeta, _, err := fetcher.FetchNotifications(context.Background(), meta, false)
		require.NoError(t, err)
		assert.Nil(t, notifs)
		assert.Equal(t, "etag-old", newMeta.ETag)
	})

	t.Run("Corrupted ETag Sanitization", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("ETag", `W/""`)
			_ = json.NewEncoder(w).Encode([]Notification{})
		}))
		defer ts.Close()

		client := NewTestClient(ts.Client(), ts.URL+"/")
		fetcher := NewNotificationFetcher(client, slog.Default())

		_, newMeta, _, err := fetcher.FetchNotifications(context.Background(), &models.SyncMeta{}, false)
		require.NoError(t, err)
		assert.Empty(t, newMeta.ETag)
	})

	t.Run("API Error Handling", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer ts.Close()

		client := NewTestClient(ts.Client(), ts.URL+"/")
		fetcher := NewNotificationFetcher(client, slog.Default())

		_, _, _, err := fetcher.FetchNotifications(context.Background(), &models.SyncMeta{}, false)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "401")
	})
}
