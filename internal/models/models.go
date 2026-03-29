package models

import (
	"fmt"
	"time"
)

// RateLimitError provides detailed context for GitHub API quota exhaustion.
type RateLimitError struct {
	Resource   string
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("github: rate limit exceeded for %s (retry after %v)", e.Resource, e.RetryAfter)
}

// RateLimitInfo encapsulates GitHub API quota metadata.
type RateLimitInfo struct {
	Limit      int
	Remaining  int
	Used       int
	Reset      time.Time
	Resource   string
	RetryAfter time.Duration
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

// EnrichmentResult holds the fetched details for a notification.
type EnrichmentResult struct {
	SubjectNodeID    string
	Body             string
	HTMLURL          string
	Author           string
	ResourceState    string
	ResourceSubState string
	FetchedAt        time.Time
}
