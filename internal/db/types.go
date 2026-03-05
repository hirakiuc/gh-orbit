package db

import (
	"log/slog"
	"time"
)

// Notification represents a GitHub notification record.
type Notification struct {
	GitHubID           string    `json:"id"`
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
	UpdatedAt          time.Time `json:"updated_at"`
}

// OrbitState represents the local-first triage state for a notification.
type OrbitState struct {
	NotificationID string `json:"notification_id"`
	Priority       int    `json:"priority"` // 0 to 3
	Status         string `json:"status"`   // 'entry', 'tracking', 'archived'
	IsReadLocally  bool   `json:"is_read_locally"`
}

// SyncMeta represents the differential sync state per user and endpoint.
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

// LogValue implements slog.LogValuer to redact sensitive sync metadata.
func (s SyncMeta) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("user_id", s.UserID),
		slog.String("key", s.Key),
		slog.String("last_modified", "<REDACTED>"),
		slog.String("etag", "<REDACTED>"),
		slog.Int("poll_interval", s.PollInterval),
	)
}
