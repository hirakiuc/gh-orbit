package triage

import (
	"database/sql"
	"time"
)

// State represents the local triage state for a notification.
type State struct {
	NotificationID string `json:"notification_id"`
	Priority       int    `json:"priority"`
	Status         string `json:"status"`
	IsReadLocally  bool   `json:"is_read_locally"`
	IsNotified     bool   `json:"is_notified"`
}

// Notification represents the core notification entity.
type Notification struct {
	GitHubID           string       `json:"github_id"`
	SubjectTitle       string       `json:"subject_title"`
	SubjectURL         string       `json:"subject_url"`
	SubjectType        string       `json:"subject_type"`
	Reason             string       `json:"reason"`
	RepositoryFullName string       `json:"repository_full_name"`
	HTMLURL            string       `json:"html_url"`
	Body               string       `json:"body"`
	AuthorLogin        string       `json:"author_login"`
	ResourceState      string       `json:"resource_state"`
	ResourceSubState   string       `json:"resource_sub_state"`
	SubjectNodeID      string       `json:"subject_node_id"`
	IsEnriched         bool         `json:"is_enriched"`
	EnrichedAt         sql.NullTime `json:"enriched_at"`
	UpdatedAt          time.Time    `json:"updated_at"`
}

// NotificationWithState is a flattened view of a notification and its local state.
type NotificationWithState struct {
	Notification
	State
}
