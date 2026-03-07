package db

import (
	"github.com/hirakiuc/gh-orbit/internal/types"
)

// Re-export common database types from the neutral internal/types package.
type Notification = types.Notification
type OrbitState = types.OrbitState
type NotificationWithState = types.NotificationWithState
type SyncMeta = types.SyncMeta
type BridgeHealth = types.BridgeHealth
