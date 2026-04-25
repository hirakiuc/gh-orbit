package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedactSecrets(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Redacts ghp token",
			input:    "token: ghp_SECRET_123_SUFFIX",
			expected: "token: <REDACTED>",
		},
		{
			name:     "Redacts github_pat token",
			input:    "pat: github_pat_11AAA_22BBB",
			expected: "pat: <REDACTED>",
		},
		{
			name:     "Redacts gho token",
			input:    "auth: gho_OAuthToken_999",
			expected: "auth: <REDACTED>",
		},
		{
			name:     "Redacts ghs token",
			input:    "session: ghs_ServerToken_000",
			expected: "session: <REDACTED>",
		},
		{
			name:     "Redacts ghr token",
			input:    "refresh: ghr_RefreshToken_111",
			expected: "refresh: <REDACTED>",
		},
		{
			name:     "Redacts multiple tokens in one string",
			input:    "first: ghp_TOKEN1, second: gho_TOKEN2",
			expected: "first: <REDACTED>, second: <REDACTED>",
		},
		{
			name:     "Does not over-redact non-tokens",
			input:    "this is a normal string with ghp_ missing underscore suffix",
			expected: "this is a normal string with ghp_ missing underscore suffix",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := RedactSecrets(tt.input)
			assert.Equal(t, tt.expected, actual)
		})
	}
}
