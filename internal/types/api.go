package types

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/models"
	"github.com/hirakiuc/gh-orbit/internal/triage"
)

// Sentinel errors for common failure modes.
var (
	ErrSyncIntervalNotReached = errors.New("sync: polling interval not reached")
)

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

// PersistenceReport represents the health of the local storage.
type PersistenceReport struct {
	ConfigPath string `json:"config_path"`
	DataPath   string `json:"data_path"`
	StatePath  string `json:"state_path"`
	TracePath  string `json:"trace_path"`
	CacheSize  string `json:"cache_size"`
}

// ConfigReport represents the health of the application configuration.
type ConfigReport struct {
	Version int    `json:"version"`
	Status  string `json:"status"` // Valid, Invalid, Missing
	Error   string `json:"error,omitempty"`
}

// BuildReport represents the application build metadata.
type BuildReport struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

// DoctorReport represents the full environment diagnostic report.
type DoctorReport struct {
	SchemaVersion int               `json:"schema_version"`
	Timestamp     time.Time         `json:"timestamp"`
	OS            string            `json:"os"`
	Arch          string            `json:"arch"`
	KernelVersion string            `json:"kernel_version"`
	BinaryPath    string            `json:"binary_path"`
	Build         BuildReport       `json:"build"`
	ActiveTier    string            `json:"active_tier"`
	FocusMode     string            `json:"focus_mode"`
	BridgeStatus  BridgeStatus      `json:"bridge_status"`
	Persistence   PersistenceReport `json:"persistence"`
	Config        ConfigReport      `json:"config"`
	Checks        []BridgeCheck     `json:"checks"`
}

// Notifier defines the interface for delivering system notifications.
type Notifier interface {
	Notify(ctx context.Context, title, subtitle, body, url string, priority int) error
	Shutdown(ctx context.Context)
	Status() BridgeStatus
}

// Syncer defines the interface for the synchronization engine.
type Syncer interface {
	Sync(ctx context.Context, userID string, force bool) (models.RateLimitInfo, error)
	Shutdown(ctx context.Context)
	BridgeStatus() BridgeStatus
}

// Enricher defines the interface for fetching notification details.
type Enricher interface {
	FetchDetail(ctx context.Context, u string, subjectType string) (models.EnrichmentResult, error)
	FetchHybridBatch(ctx context.Context, notifications []triage.NotificationWithState) map[string]models.EnrichmentResult
	Shutdown(ctx context.Context)
}

// TaskFunc represents an API operation that returns a message.
type TaskFunc func(context.Context) tea.Msg

// TrafficController defines the interface for serialized API access.
type TrafficController interface {
	Submit(priority int, fn TaskFunc) tea.Cmd
	UpdateRateLimit(ctx context.Context, info models.RateLimitInfo)
	Remaining() int
	RateLimitUpdates() chan models.RateLimitInfo
	Shutdown(ctx context.Context)
}

// CommandExecutor defines the interface for executing system commands safely.
type CommandExecutor interface {
	// Execute executes a command and returns its standard output.
	Execute(ctx context.Context, name string, args ...string) ([]byte, error)
	// Run executes a command and waits for it to complete.
	Run(ctx context.Context, name string, args ...string) error
	// InteractiveGH executes a GitHub CLI command interactively using tea.ExecProcess.
	InteractiveGH(callback func(error) tea.Msg, args ...string) tea.Cmd
}

// ErrMsg is a common error message wrapper for Bubble Tea updates.
type ErrMsg struct{ Err error }

// SyncRepository defines the database interactions required by the SyncEngine.
type SyncRepository interface {
	GetSyncMeta(ctx context.Context, userID, key string) (*models.SyncMeta, error)
	UpdateSyncMeta(ctx context.Context, s models.SyncMeta) error
	UpsertNotifications(ctx context.Context, notifications []triage.Notification) error
	GetNotification(ctx context.Context, id string) (*triage.NotificationWithState, error)
	MarkNotifiedBatch(ctx context.Context, ids []string) error
}

// EnrichmentRepository defines the database interactions required by the EnrichmentEngine.
type EnrichmentRepository interface {
	EnrichNotification(ctx context.Context, id, body, author, htmlURL, resourceState string) error
	UpdateResourceStateByNodeID(ctx context.Context, nodeID, state string) error
}

// AlertRepository defines the database interactions required by the AlertService.
type AlertRepository interface {
	ListNotifications(ctx context.Context) ([]triage.NotificationWithState, error)
	GetBridgeHealth(ctx context.Context) (*models.BridgeHealth, error)
	UpdateBridgeHealth(ctx context.Context, h models.BridgeHealth) error
}

// Repository defines the full database capabilities required by the TUI and Services.
type Repository interface {
	SyncRepository
	EnrichmentRepository
	AlertRepository

	// Triage specific
	MarkReadLocally(ctx context.Context, id string, isRead bool) error
	ArchiveThread(ctx context.Context, id string) error
	UnarchiveThread(ctx context.Context, id string) error
	MuteThread(ctx context.Context, id string) error
	UnmuteThread(ctx context.Context, id string) error
	SetPriority(ctx context.Context, id string, priority int) error
}
