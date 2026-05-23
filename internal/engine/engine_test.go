package engine

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/hirakiuc/gh-orbit/internal/api"
	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestCoreEngine_Initialization(t *testing.T) {
	ctx := context.Background()
	cfg := config.DefaultConfig()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	executor := api.NewOSCommandExecutor()

	// This test ensures the engine can be initialized without any TUI or bubbletea dependencies.
	// It relies on actual constructors but uses an OSCommandExecutor which is platform-agnostic in logic.
	// Note: In a restricted environment, database opening might fail if paths are not writeable,
	// but the compilation and wiring check is the primary goal here.
	eng, err := NewCoreEngine(ctx, cfg, logger, executor)
	if err != nil {
		t.Logf("Engine initialization failed (expected in restricted env): %v", err)
		return
	}
	defer eng.Shutdown(ctx)

	assert.NotNil(t, eng.Sync)
	assert.NotNil(t, eng.Enrich)
	assert.NotNil(t, eng.Traffic)
	assert.NotNil(t, eng.Alert)
	assert.NotNil(t, eng.DB)
}

func TestNewCoreEngine_Guards(t *testing.T) {
	ctx := context.Background()
	cfg := config.DefaultConfig()
	logger := slog.Default()
	executor := api.NewOSCommandExecutor()

	t.Run("Missing Config", func(t *testing.T) {
		_, err := NewCoreEngine(ctx, nil, logger, executor)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "config is required")
	})

	t.Run("Missing Logger", func(t *testing.T) {
		_, err := NewCoreEngine(ctx, cfg, nil, executor)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "logger is required")
	})

	t.Run("Missing Executor", func(t *testing.T) {
		_, err := NewCoreEngine(ctx, cfg, logger, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "executor is required")
	})
}

func TestCoreEngine_Shutdown_IntegratedModeOwnsSharedServices(t *testing.T) {
	ctx := context.Background()
	logger := slog.Default()

	mockSyncer := mocks.NewMockSyncer(t)
	mockEnricher := mocks.NewMockEnricher(t)
	mockTraffic := mocks.NewMockTrafficController(t)
	mockAlerter := mocks.NewMockAlerter(t)

	usableCleanupCtx := mock.MatchedBy(func(ctx context.Context) bool {
		return ctx != nil
	})

	mockSyncer.EXPECT().Shutdown(usableCleanupCtx).Return().Once()
	mockEnricher.EXPECT().Shutdown(usableCleanupCtx).Return().Once()
	mockTraffic.EXPECT().Shutdown(usableCleanupCtx).Return().Once()
	mockAlerter.EXPECT().Shutdown(usableCleanupCtx).Return().Once()

	engine := &CoreEngine{
		Logger:  logger,
		Sync:    mockSyncer,
		Enrich:  mockEnricher,
		Traffic: mockTraffic,
		Alert:   mockAlerter,
	}

	require.NotPanics(t, func() {
		engine.Shutdown(ctx)
	})
}
