package api

import (
	"github.com/hirakiuc/gh-orbit/internal/types"
)

// Re-export common types from the neutral internal/types package to maintain API package consistency.
type GHNotification = types.GHNotification
type BridgeStatus = types.BridgeStatus

const (
	StatusHealthy           = types.StatusHealthy
	StatusPermissionsDenied = types.StatusPermissionsDenied
	StatusUnsupported       = types.StatusUnsupported
	StatusBroken            = types.StatusBroken
	StatusUnknown           = types.StatusUnknown
)

type BridgeCheck = types.BridgeCheck
type DoctorReport = types.DoctorReport

// Re-export interfaces
type Fetcher = types.Fetcher
type Notifier = types.Notifier
type SyncRepository = types.SyncRepository
type EnrichmentRepository = types.EnrichmentRepository
type AlertRepository = types.AlertRepository
