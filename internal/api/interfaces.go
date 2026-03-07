package api

import (
	"context"

	"github.com/hirakiuc/gh-orbit/internal/db"
)

// SyncRepository defines the database interactions required by the SyncEngine.
type SyncRepository interface {
	GetSyncMeta(userID, key string) (*db.SyncMeta, error)
	UpdateSyncMeta(s db.SyncMeta) error
	UpsertNotification(n db.Notification) error
	GetNotification(id string) (*db.NotificationWithState, error)
	MarkNotifiedBatch(ids []string) error
}

// EnrichmentRepository defines the database interactions required by the EnrichmentEngine.
type EnrichmentRepository interface {
	EnrichNotification(ctx context.Context, id, body, author, htmlURL, resourceState string) error
	UpdateResourceStateByNodeID(ctx context.Context, nodeID, state string) error
}

// AlertRepository defines the database interactions required by the AlertService.
type AlertRepository interface {
	ListNotifications() ([]db.NotificationWithState, error)
	GetNotification(id string) (*db.NotificationWithState, error)
	GetBridgeHealth() (*db.BridgeHealth, error)
	UpdateBridgeHealth(h db.BridgeHealth) error
}
