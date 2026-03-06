package api

import (
	"log/slog"
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

// GHUser represents the GitHub API response for a user.
type GHUser struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
}

// LogValue implements slog.LogValuer to redact sensitive user data.
func (u GHUser) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Int64("id", u.ID),
		slog.String("login", "<REDACTED>"),
	)
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
	FetchNotifications(meta *db.SyncMeta, force bool) ([]GHNotification, *db.SyncMeta, int, error)
}
