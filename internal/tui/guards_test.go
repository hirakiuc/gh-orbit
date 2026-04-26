package tui

import (
	"log/slog"
	"testing"

	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/mocks"
	"github.com/hirakiuc/gh-orbit/internal/types"
	"github.com/stretchr/testify/assert"
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

	t.Run("Missing UserID", func(t *testing.T) {
		_, err := NewModel(ModelParams{Config: cfg, Logger: logger, DB: mockRepo, Syncer: mockSyncer, Enricher: mockEnricher, Alerter: mockAlerter})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user ID is required")
	})

	t.Run("Missing Config", func(t *testing.T) {
		_, err := NewModel(ModelParams{UserID: "u", Logger: logger, DB: mockRepo, Syncer: mockSyncer, Enricher: mockEnricher, Alerter: mockAlerter})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "config is required")
	})

	t.Run("Missing Logger", func(t *testing.T) {
		_, err := NewModel(ModelParams{UserID: "u", Config: cfg, DB: mockRepo, Syncer: mockSyncer, Enricher: mockEnricher, Alerter: mockAlerter})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "logger is required")
	})

	t.Run("Missing DB", func(t *testing.T) {
		_, err := NewModel(ModelParams{UserID: "u", Config: cfg, Logger: logger, Syncer: mockSyncer, Enricher: mockEnricher, Alerter: mockAlerter})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database is required")
	})

	t.Run("Missing Syncer", func(t *testing.T) {
		_, err := NewModel(ModelParams{UserID: "u", Config: cfg, Logger: logger, DB: mockRepo, Enricher: mockEnricher, Alerter: mockAlerter})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "syncer is required")
	})

	t.Run("Missing Enricher", func(t *testing.T) {
		_, err := NewModel(ModelParams{UserID: "u", Config: cfg, Logger: logger, DB: mockRepo, Syncer: mockSyncer, Alerter: mockAlerter})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "enricher is required")
	})

	t.Run("Missing Alerter", func(t *testing.T) {
		_, err := NewModel(ModelParams{UserID: "u", Config: cfg, Logger: logger, DB: mockRepo, Syncer: mockSyncer, Enricher: mockEnricher})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "alerter is required")
	})
}
