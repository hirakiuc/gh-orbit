package main

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/api"
	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/mocks"
	"github.com/hirakiuc/gh-orbit/internal/tui"
	"github.com/hirakiuc/gh-orbit/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type fakeTeaProgram struct {
	run func() (tea.Model, error)
}

func (f fakeTeaProgram) Run() (tea.Model, error) {
	return f.run()
}

type runProgramTestDeps struct {
	model    *tui.Model
	syncer   *mocks.MockSyncer
	enricher *mocks.MockEnricher
	alerter  *mocks.MockAlerter
}

func newRunProgramTestModel(t *testing.T, taskRoot context.Context, opts ...tui.Option) runProgramTestDeps {
	t.Helper()

	cfg := config.DefaultConfig()
	logger := slog.Default()
	repo := mocks.NewMockRepository(t)
	syncer := mocks.NewMockSyncer(t)
	enricher := mocks.NewMockEnricher(t)
	alerter := mocks.NewMockAlerter(t)

	syncer.EXPECT().BridgeStatus().Return(types.StatusHealthy).Maybe()
	alerter.EXPECT().BridgeStatus().Return(types.StatusHealthy).Maybe()
	backend, err := api.NewBackend("user", repo, syncer, enricher, nil, nil, nil, nil)
	assert.NoError(t, err)

	m, err := tui.NewModel(tui.ModelParams{
		UserID:   "user",
		Config:   cfg,
		Logger:   logger,
		TaskRoot: taskRoot,
		Backend:  backend,
		Alerter:  alerter,
		Options:  opts,
	})
	assert.NoError(t, err)

	return runProgramTestDeps{
		model:    m,
		syncer:   syncer,
		enricher: enricher,
		alerter:  alerter,
	}
}

func TestRunProgram_ShutsDownModelOnSuccess(t *testing.T) {
	orig := newTeaProgram
	t.Cleanup(func() { newTeaProgram = orig })

	lifecycle := api.NewAppLifecycle(context.Background())
	deps := newRunProgramTestModel(t, lifecycle.Context(), tui.WithOwnedSubsystemShutdown())
	usableCleanupCtx := mock.MatchedBy(func(ctx context.Context) bool {
		err := ctx.Err()
		_, hasDeadline := ctx.Deadline()
		return err == nil && hasDeadline
	})
	deps.syncer.EXPECT().Shutdown(usableCleanupCtx).Return().Once()
	deps.enricher.EXPECT().Shutdown(usableCleanupCtx).Return().Once()
	deps.alerter.EXPECT().Shutdown(usableCleanupCtx).Return().Once()

	newTeaProgram = func(m tea.Model, opts ...tea.ProgramOption) teaProgram {
		return fakeTeaProgram{
			run: func() (tea.Model, error) {
				return m, nil
			},
		}
	}

	err := runProgram(lifecycle, deps.model)
	assert.NoError(t, err)
}

func TestRunProgram_ShutsDownModelAfterLifecycleCancellation(t *testing.T) {
	orig := newTeaProgram
	t.Cleanup(func() { newTeaProgram = orig })

	parent, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	lifecycle := api.NewAppLifecycle(parent)
	deps := newRunProgramTestModel(t, lifecycle.Context(), tui.WithOwnedSubsystemShutdown())
	usableCleanupCtx := mock.MatchedBy(func(ctx context.Context) bool {
		err := ctx.Err()
		_, hasDeadline := ctx.Deadline()
		return err == nil && hasDeadline
	})
	deps.syncer.EXPECT().Shutdown(usableCleanupCtx).Return().Once()
	deps.enricher.EXPECT().Shutdown(usableCleanupCtx).Return().Once()
	deps.alerter.EXPECT().Shutdown(usableCleanupCtx).Return().Once()

	newTeaProgram = func(m tea.Model, opts ...tea.ProgramOption) teaProgram {
		return fakeTeaProgram{
			run: func() (tea.Model, error) {
				cancel()
				return m, nil
			},
		}
	}

	err := runProgram(lifecycle, deps.model)
	assert.NoError(t, err)
}

func TestRunProgram_ShutsDownModelOnProgramError(t *testing.T) {
	orig := newTeaProgram
	t.Cleanup(func() { newTeaProgram = orig })

	lifecycle := api.NewAppLifecycle(context.Background())
	deps := newRunProgramTestModel(t, lifecycle.Context(), tui.WithOwnedSubsystemShutdown())
	usableCleanupCtx := mock.MatchedBy(func(ctx context.Context) bool {
		err := ctx.Err()
		_, hasDeadline := ctx.Deadline()
		return err == nil && hasDeadline
	})
	deps.syncer.EXPECT().Shutdown(usableCleanupCtx).Return().Once()
	deps.enricher.EXPECT().Shutdown(usableCleanupCtx).Return().Once()
	deps.alerter.EXPECT().Shutdown(usableCleanupCtx).Return().Once()

	newTeaProgram = func(m tea.Model, opts ...tea.ProgramOption) teaProgram {
		return fakeTeaProgram{
			run: func() (tea.Model, error) {
				return m, errors.New("boom")
			},
		}
	}

	err := runProgram(lifecycle, deps.model)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "TUI error: boom")
}

func TestRunProgram_StandaloneOwnershipLeavesSubsystemShutdownToOuterLayer(t *testing.T) {
	orig := newTeaProgram
	t.Cleanup(func() { newTeaProgram = orig })

	lifecycle := api.NewAppLifecycle(context.Background())
	deps := newRunProgramTestModel(t, lifecycle.Context())

	usableCleanupCtx := mock.MatchedBy(func(ctx context.Context) bool {
		err := ctx.Err()
		_, hasDeadline := ctx.Deadline()
		return err == nil && hasDeadline
	})
	deps.syncer.EXPECT().Shutdown(usableCleanupCtx).Return().Once()
	deps.enricher.EXPECT().Shutdown(usableCleanupCtx).Return().Once()
	deps.alerter.EXPECT().Shutdown(usableCleanupCtx).Return().Once()

	newTeaProgram = func(m tea.Model, opts ...tea.ProgramOption) teaProgram {
		return fakeTeaProgram{
			run: func() (tea.Model, error) {
				return m, nil
			},
		}
	}

	err := runProgram(lifecycle, deps.model)
	assert.NoError(t, err)

	cleanupCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	deps.syncer.Shutdown(cleanupCtx)
	deps.enricher.Shutdown(cleanupCtx)
	deps.alerter.Shutdown(cleanupCtx)
}
