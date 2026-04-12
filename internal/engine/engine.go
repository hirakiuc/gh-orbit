package engine

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/hirakiuc/gh-orbit/internal/api"
	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/db"
	"github.com/hirakiuc/gh-orbit/internal/github"
	"github.com/hirakiuc/gh-orbit/internal/types"
)

// CoreEngine coordinates all headless services of gh-orbit.
type CoreEngine struct {
	Config  *config.Config
	Logger  *slog.Logger
	DB      types.Repository
	Client  github.Client
	Sync    types.Syncer
	Enrich  types.Enricher
	Traffic types.TrafficController
	Alert   api.Alerter
}

// NewCoreEngine initializes the engine with all its dependencies.
func NewCoreEngine(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	executor types.CommandExecutor,
) (*CoreEngine, error) {
	// 1. Initialize Persistence
	database, err := db.Open(ctx, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// 2. Initialize GitHub Client
	client, err := github.NewClient()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize GitHub client: %w", err)
	}

	// 3. Initialize Services
	traffic := api.NewAPITrafficController(ctx, logger)
	enricher := api.NewEnrichmentEngine(ctx, client, database, logger)
	alerter := api.NewAlertService(ctx, cfg, logger, database, executor)

	fetcher := github.NewNotificationFetcher(client, logger)
	syncer := api.NewSyncEngine(fetcher, database, alerter, logger)

	return &CoreEngine{
		Config:  cfg,
		Logger:  logger,
		DB:      database,
		Client:  client,
		Sync:    syncer,
		Enrich:  enricher,
		Traffic: traffic,
		Alert:   alerter,
	}, nil
}

// Shutdown ensures all background resources are released cleanly.
func (e *CoreEngine) Shutdown(ctx context.Context) {
	e.Sync.Shutdown(ctx)
	e.Enrich.Shutdown(ctx)
	e.Traffic.Shutdown(ctx)
	e.Alert.Shutdown(ctx)
	if closer, ok := e.DB.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
	e.Logger.InfoContext(ctx, "core engine shutdown complete")
}
