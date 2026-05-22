package main

import (
	"context"
	"errors"
	"log/slog"
	"testing"

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

func newRunProgramTestModel(t *testing.T, taskRoot context.Context) *tui.Model {
	t.Helper()

	cfg := config.DefaultConfig()
	logger := slog.Default()
	repo := mocks.NewMockRepository(t)
	syncer := mocks.NewMockSyncer(t)
	enricher := mocks.NewMockEnricher(t)
	alerter := mocks.NewMockAlerter(t)

	syncer.EXPECT().BridgeStatus().Return(types.StatusHealthy).Maybe()
	alerter.EXPECT().BridgeStatus().Return(types.StatusHealthy).Maybe()

	usableCleanupCtx := mock.MatchedBy(func(ctx context.Context) bool {
		err := ctx.Err()
		_, hasDeadline := ctx.Deadline()
		return err == nil && hasDeadline
	})
	syncer.EXPECT().Shutdown(usableCleanupCtx).Return().Once()
	enricher.EXPECT().Shutdown(usableCleanupCtx).Return().Once()
	alerter.EXPECT().Shutdown(usableCleanupCtx).Return().Once()

	m, err := tui.NewModel(tui.ModelParams{
		UserID:   "user",
		Config:   cfg,
		Logger:   logger,
		TaskRoot: taskRoot,
		DB:       repo,
		Syncer:   syncer,
		Enricher: enricher,
		Alerter:  alerter,
	})
	assert.NoError(t, err)

	return m
}

func TestRunProgram_ShutsDownModelOnSuccess(t *testing.T) {
	orig := newTeaProgram
	t.Cleanup(func() { newTeaProgram = orig })

	lifecycle := api.NewAppLifecycle(context.Background())
	m := newRunProgramTestModel(t, lifecycle.Context())

	newTeaProgram = func(m tea.Model, opts ...tea.ProgramOption) teaProgram {
		return fakeTeaProgram{
			run: func() (tea.Model, error) {
				return m, nil
			},
		}
	}

	err := runProgram(lifecycle, m)
	assert.NoError(t, err)
}

func TestRunProgram_ShutsDownModelAfterLifecycleCancellation(t *testing.T) {
	orig := newTeaProgram
	t.Cleanup(func() { newTeaProgram = orig })

	parent, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	lifecycle := api.NewAppLifecycle(parent)
	m := newRunProgramTestModel(t, lifecycle.Context())

	newTeaProgram = func(m tea.Model, opts ...tea.ProgramOption) teaProgram {
		return fakeTeaProgram{
			run: func() (tea.Model, error) {
				cancel()
				return m, nil
			},
		}
	}

	err := runProgram(lifecycle, m)
	assert.NoError(t, err)
}

func TestRunProgram_ShutsDownModelOnProgramError(t *testing.T) {
	orig := newTeaProgram
	t.Cleanup(func() { newTeaProgram = orig })

	lifecycle := api.NewAppLifecycle(context.Background())
	m := newRunProgramTestModel(t, lifecycle.Context())

	newTeaProgram = func(m tea.Model, opts ...tea.ProgramOption) teaProgram {
		return fakeTeaProgram{
			run: func() (tea.Model, error) {
				return m, errors.New("boom")
			},
		}
	}

	err := runProgram(lifecycle, m)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "TUI error: boom")
}
