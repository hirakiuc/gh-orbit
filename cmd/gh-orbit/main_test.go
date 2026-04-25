package main

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveLogLevel(t *testing.T) {
	tests := []struct {
		name      string
		baseLevel string
		isVerbose bool
		expected  slog.Level
	}{
		{
			name:      "Info level, not verbose",
			baseLevel: "info",
			isVerbose: false,
			expected:  slog.LevelInfo,
		},
		{
			name:      "Info level, verbose",
			baseLevel: "info",
			isVerbose: true,
			expected:  slog.LevelDebug,
		},
		{
			name:      "Debug level, not verbose",
			baseLevel: "debug",
			isVerbose: false,
			expected:  slog.LevelDebug,
		},
		{
			name:      "Error level, not verbose",
			baseLevel: "error",
			isVerbose: false,
			expected:  slog.LevelError,
		},
		{
			name:      "Error level, verbose",
			baseLevel: "error",
			isVerbose: true,
			expected:  slog.LevelDebug,
		},
		{
			name:      "Invalid level, not verbose defaults to info",
			baseLevel: "unknown",
			isVerbose: false,
			expected:  slog.LevelInfo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := resolveLogLevel(tt.baseLevel, tt.isVerbose)
			assert.Equal(t, tt.expected, actual)
		})
	}
}
