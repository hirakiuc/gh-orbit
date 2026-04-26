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
	Bus     *EventBus
	DB      types.Repository
	Client  github.Client
	Sync    types.Syncer
	Enrich  types.Enricher
	Traffic types.TrafficController
	Alert   api.Alerter
}

type options struct {
	silentAlerts bool
}

// Option defines a functional option for Engine configuration.
type Option func(*options)

// WithSilentAlerts prevents the engine from emitting system notifications.
func WithSilentAlerts() Option {
	return func(o *options) {
		o.silentAlerts = true
	}
}

// NewCoreEngine initializes the engine with all its dependencies.
func NewCoreEngine(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	executor types.CommandExecutor,
	opts ...Option,
) (*CoreEngine, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required for CoreEngine")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger is required for CoreEngine")
	}
	if executor == nil {
		return nil, fmt.Errorf("executor is required for CoreEngine")
	}

	var o options
	for _, opt := range opts {
		opt(&o)
	}

	// 1. Initialize Persistence
	database, err := db.Open(ctx, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// 2. Initialize GitHub Client
	client, err := github.NewClient()
	if err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("failed to initialize GitHub client: %w", err)
	}

	// 3. Initialize Services
	bus := NewEventBus()
	traffic := api.NewAPITrafficController(ctx, logger)

	enricher, err := api.NewEnrichmentEngine(ctx, api.EnrichParams{
		Client: client,
		DB:     database,
		Logger: logger,
	})
	if err != nil {
		_ = database.Close()
		return nil, err
	}
	enricher.OnMutation = func() { bus.Publish(EventEnrichmentUpdated) }

	alerter, err := api.NewAlertService(ctx, api.AlertParams{
		Config:   cfg,
		Logger:   logger,
		DB:       database,
		Executor: executor,
	})
	if err != nil {
		_ = database.Close()
		return nil, err
	}

	fetcher := github.NewNotificationFetcher(client, logger)

	// Wired services
	var syncAlerter api.Alerter = alerter
	if o.silentAlerts {
		syncAlerter = nil
	}
	syncer, err := api.NewSyncEngine(api.SyncParams{
		Fetcher: fetcher,
		DB:      database,
		Alerts:  syncAlerter,
		Logger:  logger,
	})
	if err != nil {
		_ = database.Close()
		return nil, err
	}
	syncer.OnMutation = func() { bus.Publish(EventNotificationsChanged) }

	return &CoreEngine{
		Config:  cfg,
		Logger:  logger,
		Bus:     bus,
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
	if e.Sync != nil {
		e.Sync.Shutdown(ctx)
	}
	if e.Enrich != nil {
		e.Enrich.Shutdown(ctx)
	}
	if e.Traffic != nil {
		e.Traffic.Shutdown(ctx)
	}
	if e.Alert != nil {
		e.Alert.Shutdown(ctx)
	}
	if e.DB != nil {
		if closer, ok := e.DB.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}
	e.Logger.InfoContext(ctx, "core engine shutdown complete")
}
