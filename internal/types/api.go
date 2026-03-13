package types

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/github"
	"github.com/hirakiuc/gh-orbit/internal/models"
)

// Sentinel errors for common failure modes.
var (
	ErrSyncIntervalNotReached = errors.New("sync: polling interval not reached")
)

// RateLimitError provides detailed context for GitHub API quota exhaustion.
type RateLimitError struct {
	Resource   string
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("github: rate limit exceeded for %s (retry after %v)", e.Resource, e.RetryAfter)
}

// Re-export model types for convenience
type Notification = models.Notification
type OrbitState = models.OrbitState
type NotificationWithState = models.NotificationWithState
type SyncMeta = models.SyncMeta
type BridgeHealth = models.BridgeHealth
type RateLimitInfo = models.RateLimitInfo

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

// DoctorReport represents the full environment diagnostic report.
type DoctorReport struct {
	SchemaVersion int               `json:"schema_version"`
	Timestamp     time.Time         `json:"timestamp"`
	OS            string            `json:"os"`
	Arch          string            `json:"arch"`
	KernelVersion string            `json:"kernel_version"`
	BinaryPath    string            `json:"binary_path"`
	ActiveTier    string            `json:"active_tier"`
	FocusMode     string            `json:"focus_mode"`
	BridgeStatus  BridgeStatus      `json:"bridge_status"`
	Persistence   PersistenceReport `json:"persistence"`
	Config        ConfigReport      `json:"config"`
	Checks        []BridgeCheck     `json:"checks"`
}

// Fetcher defines the interface for retrieving notifications from an external source.
type Fetcher interface {
	FetchNotifications(ctx context.Context, meta *SyncMeta, force bool) ([]github.Notification, *SyncMeta, RateLimitInfo, error)
}

// Notifier defines the interface for delivering system notifications.
type Notifier interface {
	Notify(ctx context.Context, title, subtitle, body, url string, priority int) error
	Shutdown(ctx context.Context)
	Status() BridgeStatus
}

// Syncer defines the interface for the synchronization engine.
type Syncer interface {
	Sync(ctx context.Context, userID string, force bool) (RateLimitInfo, error)
	Shutdown(ctx context.Context)
	BridgeStatus() BridgeStatus
}

// Enricher defines the interface for fetching notification details.
type Enricher interface {
	FetchDetail(ctx context.Context, u string, subjectType string) (EnrichmentResult, error)
	FetchHybridBatch(ctx context.Context, notifications []NotificationWithState) map[string]EnrichmentResult
	Shutdown(ctx context.Context)
}

// EnrichmentResult holds the fetched details for a notification.
type EnrichmentResult struct {
	Body          string
	HTMLURL       string
	Author        string
	ResourceState string
	FetchedAt     time.Time
}

// Alerter defines the interface for the high-level alerting service.
type Alerter interface {
	Notify(ctx context.Context, n github.Notification) error
	SyncStart(ctx context.Context)
	Shutdown(ctx context.Context)
	ActiveTierInfo() (string, BridgeStatus)
	TestNotify(ctx context.Context, title, subtitle, body string) error
	BridgeStatus() BridgeStatus
}

// TaskFunc represents an API operation that returns a message.
type TaskFunc func(context.Context) tea.Msg

// TrafficController defines the interface for serialized API access.
type TrafficController interface {
	Submit(priority int, fn TaskFunc) tea.Cmd
	UpdateRateLimit(ctx context.Context, info RateLimitInfo)
	Remaining() int
	RateLimitUpdates() chan RateLimitInfo
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
	GetSyncMeta(ctx context.Context, userID, key string) (*SyncMeta, error)
	UpdateSyncMeta(ctx context.Context, s SyncMeta) error
	UpsertNotification(ctx context.Context, n Notification) error
	GetNotification(ctx context.Context, id string) (*NotificationWithState, error)
	MarkNotifiedBatch(ctx context.Context, ids []string) error
}

// EnrichmentRepository defines the database interactions required by the EnrichmentEngine.
type EnrichmentRepository interface {
	EnrichNotification(ctx context.Context, id, body, author, htmlURL, resourceState string) error
	UpdateResourceStateByNodeID(ctx context.Context, nodeID, state string) error
}

// AlertRepository defines the database interactions required by the AlertService.
type AlertRepository interface {
	ListNotifications(ctx context.Context) ([]NotificationWithState, error)
	GetBridgeHealth(ctx context.Context) (*BridgeHealth, error)
	UpdateBridgeHealth(ctx context.Context, h BridgeHealth) error
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
