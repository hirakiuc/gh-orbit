package types

import (
	"context"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/db"
)

// GHNotification represents the GitHub API response for a notification.
type GHNotification struct {
	ID         string    `json:"id"`
	Reason     string    `json:"reason"`
	UpdatedAt  time.Time `json:"updated_at"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Subject struct {
		Title  string `json:"title"`
		URL    string `json:"url"`
		Type   string `json:"type"`
		NodeID string `json:"node_id"`
	} `json:"subject"`
}

// BridgeStatus represents the functional state of the native system bridge.
type BridgeStatus string

const (
	StatusHealthy           BridgeStatus = "healthy"
	StatusPermissionsDenied BridgeStatus = "permissions_denied"
	StatusUnsupported       BridgeStatus = "unsupported"
	StatusBroken            BridgeStatus = "broken"
	StatusUnknown           BridgeStatus = "unknown"
)

// BridgeCheck represents an individual diagnostic check.
type BridgeCheck struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Message string `json:"message,omitempty"`
}

// DoctorReport represents the full environment diagnostic report.
type DoctorReport struct {
	SchemaVersion int           `json:"schema_version"`
	Timestamp     time.Time     `json:"timestamp"`
	OS            string        `json:"os"`
	Arch          string        `json:"arch"`
	KernelVersion string        `json:"kernel_version"`
	BinaryPath    string        `json:"binary_path"`
	BridgeStatus  BridgeStatus  `json:"bridge_status"`
	Checks        []BridgeCheck `json:"checks"`
}

// Fetcher defines the interface for retrieving notifications from an external source.
type Fetcher interface {
	FetchNotifications(ctx context.Context, meta *db.SyncMeta, force bool) ([]GHNotification, *db.SyncMeta, int, error)
}

// Notifier defines the interface for delivering system notifications.
type Notifier interface {
	Notify(title, subtitle, body, url string, priority int) error
	Shutdown()
	Status() BridgeStatus
	Warmup() // Proactive health check
	Ready() <-chan struct{}
}

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
