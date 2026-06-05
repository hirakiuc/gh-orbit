package github

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"testing"

	"github.com/hirakiuc/gh-orbit/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNotificationFetcher_FetchNotifications(t *testing.T) {
	t.Run("Successful Fetch with Pagination", func(t *testing.T) {
		client := NewTestClient(newHTTPClient(t, func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/notifications" {
				headers := make(http.Header)
				headers.Set("Link", `<https://api.test/page2>; rel="next"`)
				headers.Set("ETag", "etag-1")
				headers.Set("Last-Modified", "Mon, 02 Jan 2006 15:04:05 MST")
				headers.Set("X-Poll-Interval", "30")

				body, err := json.Marshal([]Notification{{ID: "1"}, {ID: "2"}})
				require.NoError(t, err)
				return newJSONResponse(http.StatusOK, string(body), headers), nil
			}

			assert.Equal(t, "/page2", r.URL.Path)
			body, err := json.Marshal([]Notification{{ID: "3"}})
			require.NoError(t, err)
			return newJSONResponse(http.StatusOK, string(body), nil), nil
		}), "https://api.test/")
		fetcher := NewNotificationFetcher(client, slog.Default())

		notifs, newMeta, rl, err := fetcher.FetchNotifications(context.Background(), &models.SyncMeta{}, false)
		require.NoError(t, err)
		assert.Len(t, notifs, 3)
		assert.Equal(t, "etag-1", newMeta.ETag)
		assert.Equal(t, 30, newMeta.PollInterval)
		assert.Equal(t, 5000, rl.Remaining)
	})

	t.Run("Parses Unread State", func(t *testing.T) {
		client := NewTestClient(newHTTPClient(t, func(r *http.Request) (*http.Response, error) {
			body := `[
				{
					"id": "unread",
					"unread": true,
					"updated_at": "2026-06-05T00:00:00Z",
					"repository": {"full_name": "owner/repo"},
					"subject": {"title": "Needs review", "url": "https://api.github.com/repos/owner/repo/pulls/1", "type": "PullRequest", "node_id": "PR_kw"}
				},
				{
					"id": "read",
					"unread": false,
					"updated_at": "2026-06-05T00:01:00Z",
					"repository": {"full_name": "owner/repo"},
					"subject": {"title": "Already handled", "url": "https://api.github.com/repos/owner/repo/pulls/2", "type": "PullRequest", "node_id": "PR_kx"}
				}
			]`
			return newJSONResponse(http.StatusOK, body, nil), nil
		}), "https://api.test/")
		fetcher := NewNotificationFetcher(client, slog.Default())

		notifs, _, _, err := fetcher.FetchNotifications(context.Background(), &models.SyncMeta{}, false)

		require.NoError(t, err)
		require.Len(t, notifs, 2)
		assert.True(t, notifs[0].Unread)
		assert.False(t, notifs[1].Unread)
	})

	t.Run("304 Not Modified", func(t *testing.T) {
		client := NewTestClient(newHTTPClient(t, func(r *http.Request) (*http.Response, error) {
			assert.Equal(t, "etag-old", r.Header.Get("If-None-Match"))
			return newJSONResponse(http.StatusNotModified, "", nil), nil
		}), "https://api.test/")

		fetcher := NewNotificationFetcher(client, slog.Default())
		meta := &models.SyncMeta{ETag: "etag-old"}

		notifs, newMeta, _, err := fetcher.FetchNotifications(context.Background(), meta, false)
		require.NoError(t, err)
		assert.Nil(t, notifs)
		assert.Equal(t, "etag-old", newMeta.ETag)
	})

	t.Run("Corrupted ETag Sanitization", func(t *testing.T) {
		client := NewTestClient(newHTTPClient(t, func(r *http.Request) (*http.Response, error) {
			headers := make(http.Header)
			headers.Set("ETag", `W/""`)

			body, err := json.Marshal([]Notification{})
			require.NoError(t, err)
			return newJSONResponse(http.StatusOK, string(body), headers), nil
		}), "https://api.test/")
		fetcher := NewNotificationFetcher(client, slog.Default())

		_, newMeta, _, err := fetcher.FetchNotifications(context.Background(), &models.SyncMeta{}, false)
		require.NoError(t, err)
		assert.Empty(t, newMeta.ETag)
	})

	t.Run("API Error Handling", func(t *testing.T) {
		client := NewTestClient(newHTTPClient(t, func(r *http.Request) (*http.Response, error) {
			return newJSONResponse(http.StatusUnauthorized, "", nil), nil
		}), "https://api.test/")
		fetcher := NewNotificationFetcher(client, slog.Default())

		_, _, _, err := fetcher.FetchNotifications(context.Background(), &models.SyncMeta{}, false)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "401")
	})
}
