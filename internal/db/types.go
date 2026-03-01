package db

import "time"

// Notification represents a GitHub notification record.
type Notification struct {
	GitHubID           string    `json:"id"`
	SubjectTitle       string    `json:"subject_title"`
	SubjectType        string    `json:"subject_type"`
	Reason             string    `json:"reason"`
	RepositoryFullName string    `json:"repository_full_name"`
	HTMLURL            string    `json:"html_url"`
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
