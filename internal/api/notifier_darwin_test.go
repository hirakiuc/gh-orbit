package api

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/mocks"
	"github.com/hirakiuc/gh-orbit/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestDarwinNotifier_Notify(t *testing.T) {
	ctx := context.Background()
	logger := slog.Default()
	mockExecutor := mocks.NewMockCommandExecutor(t)

	// Expect osascript to be called via executor
	mockExecutor.EXPECT().Run(mock.Anything, "osascript", "-e", mock.MatchedBy(func(s string) bool {
		return assert.Contains(t, s, "display notification") &&
			assert.Contains(t, s, "Test Body") &&
			assert.Contains(t, s, "Test Title")
	})).Return(nil).Once()

	n := NewPlatformNotifier(ctx, mockExecutor, logger)

	err := n.Notify(ctx, "Test Title", "Test Subtitle", "Test Body", "https://url", 1)
	assert.NoError(t, err)

	// Allow time for fire-and-forget goroutine to execute
	time.Sleep(100 * time.Millisecond)
}

func TestDarwinNotifier_Status(t *testing.T) {
	n := NewPlatformNotifier(context.Background(), nil, slog.Default())
	assert.Equal(t, types.StatusHealthy, n.Status())
}
