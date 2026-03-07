package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestActions_URLParsing(t *testing.T) {
	tests := map[string]struct {
		url      string
		expected string
		isTag    bool
	}{
		"PR URL": {
			url:      "https://github.com/owner/repo/pull/123",
			expected: "123",
		},
		"Issue URL": {
			url:      "https://github.com/owner/repo/issues/456",
			expected: "456",
		},
		"Release URL": {
			url:      "https://github.com/owner/repo/releases/tag/v1.0.0",
			expected: "v1.0.0",
			isTag:    true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var got string
			if tc.isTag {
				got = extractTagFromURL(tc.url)
			} else {
				got = extractNumberFromURL(tc.url)
			}
			assert.Equal(t, tc.expected, got)
		})
	}
}

func TestActions_Validation(t *testing.T) {
	assert.True(t, isValidGitHubURL("https://github.com/owner/repo"))
	assert.True(t, isValidGitHubURL("https://api.github.com/repos/o/r"))
	assert.False(t, isValidGitHubURL("https://malicious.com/gh"))
	assert.False(t, isValidGitHubURL(""))
}
