package api

import (
	"context"
	"log/slog"

	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/github"
	"github.com/hirakiuc/gh-orbit/internal/types"
)

// Alerter defines the interface for the high-level alerting service.
type Alerter interface {
	Notify(ctx context.Context, n github.Notification) error
	SyncStart(ctx context.Context)
	Shutdown(ctx context.Context)
	ActiveTierInfo() (string, types.BridgeStatus)
	TestNotify(ctx context.Context, title, subtitle, body string) error
	BridgeStatus() types.BridgeStatus
}

// SyncParams contains dependencies for the SyncEngine.
type SyncParams struct {
	Fetcher github.Fetcher
	DB      types.SyncRepository
	Alerts  Alerter
	Logger  *slog.Logger
}

// EnrichParams contains dependencies for the EnrichmentEngine.
type EnrichParams struct {
	Client github.Client
	DB     types.EnrichmentRepository
	Logger *slog.Logger
}

// AlertParams contains dependencies for the AlertService.
type AlertParams struct {
	Config   *config.Config
	Logger   *slog.Logger
	DB       types.AlertRepository
	Executor types.CommandExecutor
}
