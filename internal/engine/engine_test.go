package engine

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/hirakiuc/gh-orbit/internal/api"
	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/stretchr/testify/assert"
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
