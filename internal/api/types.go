package api

import (
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
		Title string `json:"title"`
		URL   string `json:"url"`
		Type  string `json:"type"`
	} `json:"subject"`
}

// GHUser represents the GitHub API response for a user.
type GHUser struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
}

// Fetcher defines the interface for retrieving notifications from an external source.
type Fetcher interface {
	FetchNotifications(meta *db.SyncMeta) ([]GHNotification, *db.SyncMeta, error)
}
