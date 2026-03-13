package api

import (
	"context"

	"github.com/hirakiuc/gh-orbit/internal/github"
	"github.com/hirakiuc/gh-orbit/internal/types"
)

// Re-export common types from the neutral internal/types package to maintain API package consistency.
type GHUser = github.User
type GHNotification = github.Notification
type BridgeStatus = types.BridgeStatus
type BridgeHealth = types.BridgeHealth

const (
	StatusHealthy           = types.StatusHealthy
	StatusPermissionsDenied = types.StatusPermissionsDenied
	StatusUnsupported       = types.StatusUnsupported
	StatusBroken            = types.StatusBroken
	StatusUnknown           = types.StatusUnknown
)

type BridgeCheck = types.BridgeCheck
type PersistenceReport = types.PersistenceReport
type ConfigReport = types.ConfigReport
type DoctorReport = types.DoctorReport
type EnrichmentResult = types.EnrichmentResult

// Re-export interfaces
type Fetcher = github.Fetcher
type Notifier = types.Notifier
type SyncRepository = types.SyncRepository
type EnrichmentRepository = types.EnrichmentRepository
type AlertRepository = types.AlertRepository

// Engine Interfaces
type Syncer = types.Syncer
type Enricher = types.Enricher

// Alerter defines the interface for the high-level alerting service.
type Alerter interface {
	Notify(ctx context.Context, n github.Notification) error
	SyncStart(ctx context.Context)
	Shutdown(ctx context.Context)
	ActiveTierInfo() (string, BridgeStatus)
	TestNotify(ctx context.Context, title, subtitle, body string) error
	BridgeStatus() BridgeStatus
}

type TaskFunc = types.TaskFunc
type TrafficController = types.TrafficController
type RESTClient = github.RESTClient
type GraphQLClient = github.GraphQLClient

type CommandExecutor = types.CommandExecutor
type ErrMsg = types.ErrMsg

// Repository defines the full database capabilities required by the TUI and Services.
type Repository = types.Repository

// GitHubClient defines the operations required from the GitHub API client.
type GitHubClient = github.Client
