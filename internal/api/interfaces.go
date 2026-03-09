package api

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/types"
)

// Re-export common types from the neutral internal/types package to maintain API package consistency.
type GHUser = types.GHUser
type GHNotification = types.GHNotification
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
type Fetcher = types.Fetcher
type Notifier = types.Notifier
type SyncRepository = types.SyncRepository
type EnrichmentRepository = types.EnrichmentRepository
type AlertRepository = types.AlertRepository

// Engine Interfaces
type Syncer = types.Syncer
type Enricher = types.Enricher
type Alerter = types.Alerter
type TaskFunc = types.TaskFunc
type TrafficController = types.TrafficController
type RESTClient = types.RESTClient
type GraphQLClient = types.GraphQLClient

// CommandExecutor defines the interface for executing system commands safely.
type CommandExecutor interface {
	// Execute executes a command and returns its standard output.
	Execute(ctx context.Context, name string, args ...string) ([]byte, error)
	// Run executes a command and waits for it to complete.
	Run(ctx context.Context, name string, args ...string) error
	// InteractiveGH executes a GitHub CLI command interactively using tea.ExecProcess.
	InteractiveGH(callback func(error) tea.Msg, args ...string) tea.Cmd
}

// Repository defines the full database capabilities required by the TUI and Services.
type Repository = types.Repository

// GitHubClient defines the operations required from the GitHub API client.
type GitHubClient = types.GitHubClient
