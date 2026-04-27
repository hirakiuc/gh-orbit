package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/hirakiuc/gh-orbit/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGHClient_Methods(t *testing.T) {
	t.Run("CurrentUser with Mock Auth Skip", func(t *testing.T) {
		os.Setenv("GH_ORBIT_SKIP_AUTH", "1")
		defer os.Unsetenv("GH_ORBIT_SKIP_AUTH")

		expectedUser := &User{Login: "test-user"}
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/user", r.URL.Path)
			w.Header().Set("X-RateLimit-Remaining", "4999")
			_ = json.NewEncoder(w).Encode(expectedUser)
		}))
		defer ts.Close()

		client := NewTestClient(ts.Client(), ts.URL+"/")
		user, err := client.CurrentUser(context.Background())
		require.NoError(t, err)
		assert.Equal(t, expectedUser.Login, user.Login)
	})

	t.Run("MarkThreadAsRead", func(t *testing.T) {
		os.Setenv("GH_ORBIT_SKIP_AUTH", "1")
		defer os.Unsetenv("GH_ORBIT_SKIP_AUTH")

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPatch, r.Method)
			assert.Equal(t, "/notifications/threads/123", r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		}))
		defer ts.Close()

		client := NewTestClient(ts.Client(), ts.URL+"/")
		err := client.MarkThreadAsRead(context.Background(), "123")
		require.NoError(t, err)
	})

	t.Run("RateLimit Reporting", func(t *testing.T) {
		updates := make(chan models.RateLimitInfo, 1)
		client := &ghClient{rateLimitUpdates: updates}

		info := models.RateLimitInfo{Remaining: 100}
		client.ReportRateLimit(info)

		select {
		case received := <-updates:
			assert.Equal(t, 100, received.Remaining)
		default:
			t.Fatal("expected rate limit update")
		}
	})
}

func TestGHClient_Accessors(t *testing.T) {
	client := &ghClient{baseURL: "https://api.test/"}
	assert.Equal(t, "https://api.test/", client.BaseURL())
	assert.Nil(t, client.HTTP())
	assert.Nil(t, client.REST())
	assert.Nil(t, client.GQL())
}
