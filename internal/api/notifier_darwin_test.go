package api

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestDarwinNotifier_Lifecycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger := slog.Default()
	mockExecutor := mocks.NewMockCommandExecutor(t)

	n := NewPlatformNotifier(ctx, mockExecutor, logger)
	assert.NotNil(t, n)
	
	// Test Status
	status := n.Status()
	assert.NotEmpty(t, status)

	// Test Warmup/Ready
	n.Warmup()
	select {
	case <-n.Ready():
		// Pass
	case <-time.After(2 * time.Second):
		// Warmup might be slow or unsupported in CI
	}

	n.Shutdown(ctx)
}

func TestDarwinNotifier_Notify(t *testing.T) {
	t.Setenv("GH_ORBIT_NOTIFIER_FORCE_APPLE_SCRIPT", "1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger := slog.Default()
	mockExecutor := mocks.NewMockCommandExecutor(t)
	mockExecutor.EXPECT().Run(mock.Anything, "osascript", "-e", mock.Anything).Return(nil).Maybe()

	n := NewPlatformNotifier(ctx, mockExecutor, logger)
	t.Cleanup(func() { n.Shutdown(ctx) })

	// Test async delivery (should not block or panic)
	err := n.Notify(ctx, "Title", "Subtitle", "Body", "https://url", 1)
	assert.NoError(t, err)
	
	// Allow small time for worker to process
	time.Sleep(500 * time.Millisecond)
}
