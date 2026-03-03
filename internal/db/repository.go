package db

import (
	"database/sql"
	"fmt"
)

// UpsertMetadata inserts or updates core notification metadata from API polling.
func (db *DB) UpsertMetadata(n Notification) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// 1. Upsert notification metadata (API fields only)
	_, err = tx.Exec(`
		INSERT INTO notifications (
			github_id, subject_title, subject_url, subject_type, reason, repository_full_name, html_url, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(github_id) DO UPDATE SET
			subject_title = excluded.subject_title,
			subject_url = excluded.subject_url,
			subject_type = excluded.subject_type,
			reason = excluded.reason,
			repository_full_name = excluded.repository_full_name,
			html_url = excluded.html_url,
			updated_at = excluded.updated_at
	`, n.GitHubID, n.SubjectTitle, n.SubjectURL, n.SubjectType, n.Reason, n.RepositoryFullName, n.HTMLURL, n.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to upsert metadata: %w", err)
	}

	// 2. Ensure orbit_state exists
	_, err = tx.Exec(`
		INSERT INTO orbit_state (notification_id, priority, status, is_read_locally)
		VALUES (?, 0, 'entry', FALSE)
		ON CONFLICT(notification_id) DO NOTHING
	`, n.GitHubID)
	if err != nil {
		return fmt.Errorf("failed to ensure orbit state: %w", err)
	}

	return tx.Commit()
}

// EnrichNotification updates a notification with detailed content (body, author).
func (db *DB) EnrichNotification(id, body, author, htmlURL string) error {
	_, err := db.Exec(`
		UPDATE notifications
		SET body = ?,
		    author_login = ?,
		    html_url = COALESCE(NULLIF(?, ''), html_url),
		    is_enriched = TRUE
		WHERE github_id = ?
	`, body, author, htmlURL, id)
	if err != nil {
		return fmt.Errorf("failed to enrich notification: %w", err)
	}
	return nil
}

// UpsertNotification is a compatibility helper that performs a metadata upsert.
func (db *DB) UpsertNotification(n Notification) error {
	return db.UpsertMetadata(n)
}

// GetNotification retrieves a notification and its local state.
type NotificationWithState struct {
	Notification
	OrbitState
}

func (db *DB) GetNotification(id string) (*NotificationWithState, error) {
	row := db.QueryRow(`
		SELECT
			n.github_id, n.subject_title, n.subject_url, n.subject_type, n.reason, n.repository_full_name, n.html_url, COALESCE(n.body, ''), COALESCE(n.author_login, ''), n.is_enriched, n.updated_at,
			s.priority, s.status, s.is_read_locally
		FROM notifications n
		JOIN orbit_state s ON n.github_id = s.notification_id
		WHERE n.github_id = ?
	`, id)

	var ns NotificationWithState
	err := row.Scan(
		&ns.GitHubID, &ns.SubjectTitle, &ns.SubjectURL, &ns.SubjectType, &ns.Reason, &ns.RepositoryFullName, &ns.HTMLURL, &ns.Body, &ns.AuthorLogin, &ns.IsEnriched, &ns.UpdatedAt,
		&ns.Priority, &ns.Status, &ns.IsReadLocally,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ns, nil
}

// ListNotifications returns all notifications with their state.
func (db *DB) ListNotifications() ([]NotificationWithState, error) {
	rows, err := db.Query(`
		SELECT
			n.github_id, n.subject_title, n.subject_url, n.subject_type, n.reason, n.repository_full_name, n.html_url, COALESCE(n.body, ''), COALESCE(n.author_login, ''), n.is_enriched, n.updated_at,
			s.priority, s.status, s.is_read_locally
		FROM notifications n
		JOIN orbit_state s ON n.github_id = s.notification_id
		ORDER BY n.updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var results []NotificationWithState
	for rows.Next() {
		var ns NotificationWithState
		err := rows.Scan(
			&ns.GitHubID, &ns.SubjectTitle, &ns.SubjectURL, &ns.SubjectType, &ns.Reason, &ns.RepositoryFullName, &ns.HTMLURL, &ns.Body, &ns.AuthorLogin, &ns.IsEnriched, &ns.UpdatedAt,
			&ns.Priority, &ns.Status, &ns.IsReadLocally,
		)
		if err != nil {
			return nil, err
		}
		results = append(results, ns)
	}
	return results, nil
}

// UpdateOrbitState updates the local triage state.
func (db *DB) UpdateOrbitState(state OrbitState) error {
	_, err := db.Exec(`
		UPDATE orbit_state
		SET priority = ?, status = ?, is_read_locally = ?
		WHERE notification_id = ?
	`, state.Priority, state.Status, state.IsReadLocally, state.NotificationID)
	return err
}

// GetSyncMeta retrieves the sync metadata for a user and endpoint.
func (db *DB) GetSyncMeta(userID, key string) (*SyncMeta, error) {
	row := db.QueryRow(`
		SELECT user_id, key, last_modified, etag, poll_interval, last_sync_at, last_error, last_error_at
		FROM sync_meta
		WHERE user_id = ? AND key = ?
	`, userID, key)

	var s SyncMeta
	err := row.Scan(&s.UserID, &s.Key, &s.LastModified, &s.ETag, &s.PollInterval, &s.LastSyncAt, &s.LastError, &s.LastErrorAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// UpdateSyncMeta updates the sync metadata.
func (db *DB) UpdateSyncMeta(s SyncMeta) error {
	_, err := db.Exec(`
		INSERT INTO sync_meta (user_id, key, last_modified, etag, poll_interval, last_sync_at, last_error, last_error_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, key) DO UPDATE SET
			last_modified = excluded.last_modified,
			etag = excluded.etag,
			poll_interval = excluded.poll_interval,
			last_sync_at = excluded.last_sync_at,
			last_error = excluded.last_error,
			last_error_at = excluded.last_error_at
	`, s.UserID, s.Key, s.LastModified, s.ETag, s.PollInterval, s.LastSyncAt, s.LastError, s.LastErrorAt)
	return err
}
