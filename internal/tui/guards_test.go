package tui

import (
	"context"
	"log/slog"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/api"
	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/mocks"
	"github.com/hirakiuc/gh-orbit/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestNewModel_Guards(t *testing.T) {
	cfg := config.DefaultConfig()
	logger := slog.Default()
	mockRepo := mocks.NewMockRepository(t)
	mockSyncer := mocks.NewMockSyncer(t)
	mockEnricher := mocks.NewMockEnricher(t)
	mockAlerter := mocks.NewMockAlerter(t)

	mockSyncer.EXPECT().BridgeStatus().Return(types.StatusHealthy).Maybe()
	mockAlerter.EXPECT().BridgeStatus().Return(types.StatusHealthy).Maybe()

	appBackend, err := api.NewAppBackend(api.AppBackendParams{
		UserID:   "u",
		Store:    mockRepo,
		Syncer:   mockSyncer,
		Enricher: mockEnricher,
	})
	assert.NoError(t, err)

	t.Run("Missing UserID", func(t *testing.T) {
		_, err := NewModel(ModelParams{Config: cfg, Logger: logger, TaskRoot: context.Background(), Backend: appBackend, Alerter: mockAlerter})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user ID is required")
	})

	t.Run("Missing Config", func(t *testing.T) {
		_, err := NewModel(ModelParams{UserID: "u", Logger: logger, TaskRoot: context.Background(), Backend: appBackend, Alerter: mockAlerter})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "config is required")
	})

	t.Run("Missing Logger", func(t *testing.T) {
		_, err := NewModel(ModelParams{UserID: "u", Config: cfg, TaskRoot: context.Background(), Backend: appBackend, Alerter: mockAlerter})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "logger is required")
	})

	t.Run("Missing TaskRoot", func(t *testing.T) {
		_, err := NewModel(ModelParams{UserID: "u", Config: cfg, Logger: logger, Backend: appBackend, Alerter: mockAlerter})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "task root context is required")
	})

	t.Run("Missing Backend", func(t *testing.T) {
		_, err := NewModel(ModelParams{UserID: "u", Config: cfg, Logger: logger, TaskRoot: context.Background(), Alerter: mockAlerter})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "backend is required")
	})

	t.Run("Missing Alerter", func(t *testing.T) {
		_, err := NewModel(ModelParams{UserID: "u", Config: cfg, Logger: logger, TaskRoot: context.Background(), Backend: appBackend})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "alerter is required")
	})
}

func TestModel_NewTaskContext_UsesTaskRootCancellation(t *testing.T) {
	taskRoot, cancel := context.WithCancel(context.Background())
	m := newTestModelWithTaskRoot(t, taskRoot)

	ctx, release := m.newTaskContext("notifications:sync", 0)
	t.Cleanup(release)

	assert.NoError(t, ctx.Err())
	cancel()

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("expected scoped task context to be canceled when task root is canceled")
	}
}

func TestModel_SubmitBackendTaskSupersedesAndCleansUpScope(t *testing.T) {
	m := newTestModel(t)
	firstStarted := make(chan context.Context, 1)
	firstDone := make(chan tea.Msg, 1)
	first := m.submitBackendTask("notification-batch", 0, func(ctx context.Context) any {
		firstStarted <- ctx
		<-ctx.Done()
		return ctx.Err()
	})
	go func() { firstDone <- first() }()
	firstCtx := <-firstStarted

	second := m.submitBackendTask("notification-batch", 0, func(ctx context.Context) any {
		return ctx.Err()
	})
	assert.Nil(t, second())
	assert.Equal(t, context.Canceled, <-firstDone)
	assert.ErrorIs(t, firstCtx.Err(), context.Canceled)

	m.taskCancelMu.Lock()
	assert.Empty(t, m.taskCancels)
	m.taskCancelMu.Unlock()
}

func TestModel_SubmitBackendTaskHonorsTaskRootCancellation(t *testing.T) {
	taskRoot, cancel := context.WithCancel(context.Background())
	m := newTestModelWithTaskRoot(t, taskRoot)
	started := make(chan struct{})
	done := make(chan tea.Msg, 1)
	cmd := m.submitBackendTask("notification-batch", 0, func(ctx context.Context) any {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	go func() { done <- cmd() }()
	<-started
	cancel()
	assert.Equal(t, context.Canceled, <-done)

	m.taskCancelMu.Lock()
	assert.Empty(t, m.taskCancels)
	m.taskCancelMu.Unlock()
}

func TestModel_Shutdown_ConnectedModeAllowsNilTraffic(t *testing.T) {
	m := newTestModel(t)
	m.traffic = nil
	m.ownsSubsystems = true
	usableCleanupCtx := mock.MatchedBy(func(ctx context.Context) bool {
		err := ctx.Err()
		_, hasDeadline := ctx.Deadline()
		return err == nil && hasDeadline
	})
	m.alerter.(*mocks.MockAlerter).EXPECT().Shutdown(usableCleanupCtx).Return().Once()

	m.Shutdown()
	testSyncer(m).AssertNotCalled(t, "Shutdown", mock.Anything)
	testEnricher(m).AssertNotCalled(t, "Shutdown", mock.Anything)
}

func TestModel_Shutdown_StandaloneModeDoesNotShutdownEngineOwnedSubsystems(t *testing.T) {
	taskRoot, cancel := context.WithCancel(context.Background())
	m := newTestModelWithTaskRoot(t, taskRoot)
	cancel()

	m.Shutdown()
}
