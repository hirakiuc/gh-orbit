package github

import (
	"net/http"
	"testing"

	"github.com/hirakiuc/gh-orbit/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestUtils_Parsing(t *testing.T) {
	t.Run("ParseRateLimitInfo", func(t *testing.T) {
		h := http.Header{}
		h.Set("X-RateLimit-Limit", "5000")
		h.Set("X-RateLimit-Remaining", "4999")
		h.Set("X-RateLimit-Used", "1")
		h.Set("X-RateLimit-Reset", "1777186849")

		rl := ParseRateLimitInfo(h)
		assert.Equal(t, 5000, rl.Limit)
		assert.Equal(t, 4999, rl.Remaining)
		assert.Equal(t, 1, rl.Used)
		assert.False(t, rl.Reset.IsZero())
	})

	t.Run("ParseLinkHeader", func(t *testing.T) {
		link := `<https://api.test/page2>; rel="next", <https://api.test/page1>; rel="prev"`
		links := ParseLinkHeader(link)
		assert.Equal(t, "https://api.test/page2", links["next"])
		assert.Equal(t, "https://api.test/page1", links["prev"])
	})
}

func TestUtils_URL_Extraction(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{"https://api.github.com/repos/owner/repo/issues/123", "123"},
		{"https://github.com/owner/repo/pull/456", "456"},
		{"invalid-url", ""},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, ExtractNumberFromURL(tt.url))
	}
}

func TestUtils_MapHTTPError(t *testing.T) {
	assert.ErrorIs(t, MapHTTPError(401), types.ErrUnauthorized)
	assert.ErrorIs(t, MapHTTPError(403), types.ErrRateLimited)
	assert.ErrorIs(t, MapHTTPError(500), types.ErrInternalServerError)
	assert.NoError(t, MapHTTPError(200))
}
