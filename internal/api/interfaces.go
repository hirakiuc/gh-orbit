package api

import (
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

type CommandExecutor = types.CommandExecutor
type ErrMsg = types.ErrMsg

// Repository defines the full database capabilities required by the TUI and Services.
type Repository = types.Repository

// GitHubClient defines the operations required from the GitHub API client.
type GitHubClient = types.GitHubClient
