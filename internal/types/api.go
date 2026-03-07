package types

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"time"

	gh "github.com/cli/go-gh/v2/pkg/api"
	tea "charm.land/bubbletea/v2"
)

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

// GitHubClient defines the operations required from the GitHub API client.
type GitHubClient interface {
	CurrentUser(ctx context.Context) (*GHUser, error)
	MarkThreadAsRead(ctx context.Context, threadID string) error
	REST() *gh.RESTClient
	GQL() *gh.GraphQLClient
	HTTP() *http.Client
	BaseURL() string
}

// Notification represents the core notification entity.
type Notification struct {
	GitHubID           string    `json:"github_id"`
	SubjectTitle       string    `json:"subject_title"`
	SubjectURL         string    `json:"subject_url"`
	SubjectType        string    `json:"subject_type"`
	Reason             string    `json:"reason"`
	RepositoryFullName string    `json:"repository_full_name"`
	HTMLURL            string    `json:"html_url"`
	Body               string    `json:"body"`
	AuthorLogin        string    `json:"author_login"`
	ResourceState      string    `json:"resource_state"`
	SubjectNodeID      string    `json:"subject_node_id"`
	IsEnriched         bool      `json:"is_enriched"`
	EnrichedAt         sql.NullTime `json:"enriched_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// OrbitState represents the local triage state for a notification.
type OrbitState struct {
	NotificationID string `json:"notification_id"`
	Priority       int    `json:"priority"`
	Status         string `json:"status"`
	IsReadLocally  bool   `json:"is_read_locally"`
	IsNotified     bool   `json:"is_notified"`
}

// NotificationWithState is a flattened view of a notification and its local state.
type NotificationWithState struct {
	Notification
	OrbitState
}

// SyncMeta tracks the synchronization state for a specific user and endpoint.
type SyncMeta struct {
	UserID       string    `json:"user_id"`
	Key          string    `json:"key"`
	LastModified string    `json:"last_modified"`
	ETag         string    `json:"etag"`
	PollInterval int       `json:"poll_interval"`
	LastSyncAt   time.Time `json:"last_sync_at"`
	LastError    string    `json:"last_error"`
	LastErrorAt  time.Time `json:"last_error_at"`
}

// BridgeHealth caches the functional state of the system bridge.
type BridgeHealth struct {
	Status        string    `json:"status"`
	OSVersion     string    `json:"os_version"`
	BinaryPath    string    `json:"binary_path"`
	BinaryVersion string    `json:"binary_version"`
	UpdatedAt     time.Time `json:"updated_at"`
}

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
	FetchNotifications(ctx context.Context, meta *SyncMeta, force bool) ([]GHNotification, *SyncMeta, int, error)
}

// Notifier defines the interface for delivering system notifications.
type Notifier interface {
	Notify(ctx context.Context, title, subtitle, body, url string, priority int) error
	Shutdown(ctx context.Context)
	Status() BridgeStatus
	Warmup() // Proactive health check
	Ready() <-chan struct{}
}

// Syncer defines the interface for the synchronization engine.
type Syncer interface {
	Sync(ctx context.Context, userID string, force bool) (int, error)
	Shutdown(ctx context.Context)
	BridgeStatus() BridgeStatus
}

// Enricher defines the interface for fetching notification details.
type Enricher interface {
	FetchDetail(ctx context.Context, u string, subjectType string) (EnrichmentResult, error)
	FetchHybridBatch(ctx context.Context, notifications []NotificationWithState) map[string]EnrichmentResult
	GetEnrichmentCmd(id, u, subjectType string, successMsg func(EnrichmentResult) tea.Msg, errorMsg func(error) tea.Msg) tea.Cmd
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
	Notify(ctx context.Context, n GHNotification) error
	SyncStart(ctx context.Context)
	Shutdown(ctx context.Context)
	Ready() <-chan struct{}
	Warmup()
	ActiveTierInfo() (string, BridgeStatus)
	TestNotify(ctx context.Context, title, subtitle, body string) error
	BridgeStatus() BridgeStatus
}

// TrafficController defines the interface for serialized API access.
type TrafficController interface {
	Submit(priority int, fn func(ctx context.Context) tea.Msg) tea.Cmd
	UpdateRateLimit(ctx context.Context, remaining int)
	Remaining() int
	Shutdown(ctx context.Context)
}

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
	GetNotification(ctx context.Context, id string) (*NotificationWithState, error)
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
