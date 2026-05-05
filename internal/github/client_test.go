package github

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/hirakiuc/gh-orbit/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGHClient_Methods(t *testing.T) {
	t.Run("CurrentUser with Mock Auth Skip", func(t *testing.T) {
		t.Setenv("GH_ORBIT_SKIP_AUTH", "1")

		expectedUser := &User{Login: "test-user"}
		client := NewTestClient(newHTTPClient(t, func(r *http.Request) (*http.Response, error) {
			assert.Equal(t, "/user", r.URL.Path)
			headers := make(http.Header)
			headers.Set("X-RateLimit-Remaining", "4999")

			body, err := json.Marshal(expectedUser)
			require.NoError(t, err)
			return newJSONResponse(http.StatusOK, string(body), headers), nil
		}), "https://api.test/")
		user, err := client.CurrentUser(context.Background())
		require.NoError(t, err)
		assert.Equal(t, expectedUser.Login, user.Login)
	})

	t.Run("CurrentUser returns request construction error for malformed base URL", func(t *testing.T) {
		t.Setenv("GH_ORBIT_SKIP_AUTH", "1")

		client := NewTestClient(http.DefaultClient, "://bad-base/")
		user, err := client.CurrentUser(context.Background())
		require.Error(t, err)
		assert.Nil(t, user)
	})

	t.Run("MarkThreadAsRead", func(t *testing.T) {
		t.Setenv("GH_ORBIT_SKIP_AUTH", "1")

		client := NewTestClient(newHTTPClient(t, func(r *http.Request) (*http.Response, error) {
			assert.Equal(t, http.MethodPatch, r.Method)
			assert.Equal(t, "/notifications/threads/123", r.URL.Path)
			return newJSONResponse(http.StatusNoContent, "", nil), nil
		}), "https://api.test/")

		err := client.MarkThreadAsRead(context.Background(), "123")
		require.NoError(t, err)
	})

	t.Run("MarkThreadAsRead returns request construction error for malformed base URL", func(t *testing.T) {
		t.Setenv("GH_ORBIT_SKIP_AUTH", "1")

		client := NewTestClient(http.DefaultClient, "://bad-base/")
		err := client.MarkThreadAsRead(context.Background(), "123")
		require.Error(t, err)
	})

	t.Run("RateLimit Reporting", func(t *testing.T) {
		updates := make(chan models.RateLimitInfo, 1)
		client := &ghClient{
			rateLimitReporter: func(info models.RateLimitInfo) {
				updates <- info
			},
		}

		info := models.RateLimitInfo{Remaining: 100}
		client.ReportRateLimit(info)

		select {
		case received := <-updates:
			assert.Equal(t, 100, received.Remaining)
		default:
			t.Fatal("expected rate limit update")
		}
	})

	t.Run("RateLimit Reporting Without Reporter Is Noop", func(t *testing.T) {
		client := &ghClient{}
		assert.NotPanics(t, func() {
			client.ReportRateLimit(models.RateLimitInfo{Remaining: 100})
		})
	})
}

func TestGHClient_Accessors(t *testing.T) {
	client := &ghClient{baseURL: "https://api.test/"}
	assert.Equal(t, "https://api.test/", client.BaseURL())
	assert.Nil(t, client.HTTP())
	assert.Nil(t, client.REST())
	assert.Nil(t, client.GQL())
}
