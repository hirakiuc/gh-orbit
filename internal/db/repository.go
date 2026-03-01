package db

import (
	"database/sql"
	"fmt"
)

// UpsertNotification inserts or updates a notification and ensures orbit state exists.
func (db *DB) UpsertNotification(n Notification) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Upsert notification
	_, err = tx.Exec(`
		INSERT INTO notifications (
			github_id, subject_title, subject_type, reason, repository_full_name, html_url, is_enriched, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(github_id) DO UPDATE SET
			subject_title = excluded.subject_title,
			subject_type = excluded.subject_type,
			reason = excluded.reason,
			repository_full_name = excluded.repository_full_name,
			html_url = excluded.html_url,
			is_enriched = excluded.is_enriched,
			updated_at = excluded.updated_at
	`, n.GitHubID, n.SubjectTitle, n.SubjectType, n.Reason, n.RepositoryFullName, n.HTMLURL, n.IsEnriched, n.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to upsert notification: %w", err)
	}

	// 2. Ensure orbit_state exists (do nothing if it does)
	_, err = tx.Exec(`
		INSERT INTO orbit_state (notification_id)
		VALUES (?)
		ON CONFLICT(notification_id) DO NOTHING
	`, n.GitHubID)
	if err != nil {
		return fmt.Errorf("failed to ensure orbit state: %w", err)
	}

	return tx.Commit()
}

// GetNotification retrieves a notification and its local state.
type NotificationWithState struct {
	Notification
	OrbitState
}

func (db *DB) GetNotification(id string) (*NotificationWithState, error) {
	row := db.QueryRow(`
		SELECT
			n.github_id, n.subject_title, n.subject_type, n.reason, n.repository_full_name, n.html_url, n.is_enriched, n.updated_at,
			s.priority, s.status, s.is_read_locally
		FROM notifications n
		JOIN orbit_state s ON n.github_id = s.notification_id
		WHERE n.github_id = ?
	`, id)

	var ns NotificationWithState
	err := row.Scan(
		&ns.GitHubID, &ns.SubjectTitle, &ns.SubjectType, &ns.Reason, &ns.RepositoryFullName, &ns.HTMLURL, &ns.IsEnriched, &ns.UpdatedAt,
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
			n.github_id, n.subject_title, n.subject_type, n.reason, n.repository_full_name, n.html_url, n.is_enriched, n.updated_at,
			s.priority, s.status, s.is_read_locally
		FROM notifications n
		JOIN orbit_state s ON n.github_id = s.notification_id
		ORDER BY n.updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []NotificationWithState
	for rows.Next() {
		var ns NotificationWithState
		err := rows.Scan(
			&ns.GitHubID, &ns.SubjectTitle, &ns.SubjectType, &ns.Reason, &ns.RepositoryFullName, &ns.HTMLURL, &ns.IsEnriched, &ns.UpdatedAt,
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
