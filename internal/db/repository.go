package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/models"
	"github.com/hirakiuc/gh-orbit/internal/triage"
)

// EnrichNotification updates a notification with detailed content (body, author).
// It also propagates the state to all notifications sharing the same subject_node_id for consistency.
func (db *DB) EnrichNotification(ctx context.Context, id, body, author, htmlURL, resourceState string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// 1. Get the subject_node_id for this notification
	var nodeID string
	err = tx.QueryRowContext(ctx, "SELECT subject_node_id FROM notifications WHERE github_id = ?", id).Scan(&nodeID)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to fetch node_id during enrichment: %w", err)
	}

	now := time.Now()

	// 2. Update the primary target
	_, err = tx.ExecContext(ctx, `
		UPDATE notifications
		SET body = ?,
		    author_login = ?,
		    html_url = COALESCE(NULLIF(?, ''), html_url),
		    resource_state = ?,
		    is_enriched = TRUE,
		    enriched_at = ?
		WHERE github_id = ?
	`, body, author, htmlURL, resourceState, now, id)
	if err != nil {
		return fmt.Errorf("failed to enrich notification: %w", err)
	}

	// 3. Propagate to peers sharing the same subject (visual continuity win!)
	if nodeID != "" {
		_, err = tx.ExecContext(ctx, `
			UPDATE notifications
			SET resource_state = ?,
			    body = CASE WHEN body = '' THEN ? ELSE body END,
			    author_login = CASE WHEN author_login = '' THEN ? ELSE author_login END,
			    is_enriched = CASE WHEN body != '' THEN TRUE ELSE is_enriched END,
			    enriched_at = ?
			WHERE subject_node_id = ? AND github_id != ?
		`, resourceState, body, author, now, nodeID, id)
		if err != nil {
			db.logger.WarnContext(ctx, "failed to propagate enrichment to peers", "node_id", nodeID, "error", err)
		}
	}

	return tx.Commit()
}

// UpdateResourceStateByNodeID updates the live status of all resources sharing a GraphQL ID.
func (db *DB) UpdateResourceStateByNodeID(ctx context.Context, nodeID, state string) error {
	db.logger.DebugContext(ctx, "db: updating resource state by node_id", "node_id", nodeID, "state", state)
	_, err := db.ExecContext(ctx, `
		UPDATE notifications
		SET resource_state = ?,
		    enriched_at = ?
		WHERE subject_node_id = ?
	`, state, time.Now(), nodeID)
	if err != nil {
		return fmt.Errorf("failed to update resource state by node_id: %w", err)
	}
	return nil
}

// UpdateSubjectNodeID specifically updates the GraphQL Global Node ID for a resource.
func (db *DB) UpdateSubjectNodeID(ctx context.Context, id, nodeID string) error {
	_, err := db.ExecContext(ctx, `
		UPDATE notifications
		SET subject_node_id = ?
		WHERE github_id = ?
	`, nodeID, id)
	return err
}

// UpsertNotifications performs batch upsert of notifications in a single transaction.
func (db *DB) UpsertNotifications(ctx context.Context, notifications []triage.Notification) error {
	if len(notifications) == 0 {
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmtNotifications, err := tx.PrepareContext(ctx, `
		INSERT INTO notifications (
			github_id, subject_title, subject_url, subject_type, reason, repository_full_name, html_url, subject_node_id, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(github_id) DO UPDATE SET
			subject_title = excluded.subject_title,
			subject_url = excluded.subject_url,
			subject_type = excluded.subject_type,
			reason = excluded.reason,
			repository_full_name = excluded.repository_full_name,
			html_url = excluded.html_url,
			subject_node_id = COALESCE(NULLIF(excluded.subject_node_id, ''), notifications.subject_node_id),
			updated_at = excluded.updated_at
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare notifications stmt: %w", err)
	}
	defer func() { _ = stmtNotifications.Close() }()

	stmtState, err := tx.PrepareContext(ctx, `
		INSERT INTO orbit_state (notification_id, priority, status, is_read_locally)
		VALUES (?, 0, 'entry', FALSE)
		ON CONFLICT(notification_id) DO NOTHING
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare orbit_state stmt: %w", err)
	}
	defer func() { _ = stmtState.Close() }()

	for _, n := range notifications {
		if _, err := stmtNotifications.ExecContext(ctx,
			n.GitHubID, n.SubjectTitle, n.SubjectURL, n.SubjectType, n.Reason,
			n.RepositoryFullName, n.HTMLURL, n.SubjectNodeID, n.UpdatedAt,
		); err != nil {
			return fmt.Errorf("failed to upsert metadata for %s: %w", n.GitHubID, err)
		}

		if _, err := stmtState.ExecContext(ctx, n.GitHubID); err != nil {
			return fmt.Errorf("failed to ensure orbit state for %s: %w", n.GitHubID, err)
		}
	}

	return tx.Commit()
}

func baseNotificationSelect() string {
	return `
		SELECT
			n.github_id, n.subject_title, n.subject_url, n.subject_type, n.reason, n.repository_full_name, n.html_url,
			COALESCE(n.body, ''), COALESCE(n.author_login, ''), COALESCE(n.resource_state, ''), COALESCE(n.subject_node_id, ''),
			n.is_enriched, n.enriched_at, n.updated_at,
			s.priority, s.status, s.is_read_locally, s.is_notified
		FROM notifications n
		JOIN orbit_state s ON n.github_id = s.notification_id
	`
}

func (db *DB) GetNotification(ctx context.Context, id string) (*triage.NotificationWithState, error) {
	query := baseNotificationSelect() + " WHERE n.github_id = ?"
	row := db.QueryRowContext(ctx, query, id)

	var ns triage.NotificationWithState
	err := row.Scan(
		&ns.GitHubID, &ns.SubjectTitle, &ns.SubjectURL, &ns.SubjectType, &ns.Reason, &ns.RepositoryFullName, &ns.HTMLURL,
		&ns.Body, &ns.AuthorLogin, &ns.ResourceState, &ns.SubjectNodeID, &ns.IsEnriched, &ns.EnrichedAt, &ns.UpdatedAt,
		&ns.Priority, &ns.Status, &ns.IsReadLocally, &ns.IsNotified,
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
func (db *DB) ListNotifications(ctx context.Context) ([]triage.NotificationWithState, error) {
	query := baseNotificationSelect() + " ORDER BY n.updated_at DESC"
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var results []triage.NotificationWithState
	for rows.Next() {
		var ns triage.NotificationWithState
		err := rows.Scan(
			&ns.GitHubID, &ns.SubjectTitle, &ns.SubjectURL, &ns.SubjectType, &ns.Reason, &ns.RepositoryFullName, &ns.HTMLURL,
			&ns.Body, &ns.AuthorLogin, &ns.ResourceState, &ns.SubjectNodeID, &ns.IsEnriched, &ns.EnrichedAt, &ns.UpdatedAt,
			&ns.Priority, &ns.Status, &ns.IsReadLocally, &ns.IsNotified,
		)
		if err != nil {
			return nil, err
		}
		results = append(results, ns)
	}
	return results, nil
}

// UpdateOrbitState updates the local triage state.
func (db *DB) UpdateOrbitState(ctx context.Context, state triage.State) error {
	_, err := db.ExecContext(ctx, `
		UPDATE orbit_state
		SET priority = ?, status = ?, is_read_locally = ?, is_notified = ?
		WHERE notification_id = ?
	`, state.Priority, state.Status, state.IsReadLocally, state.IsNotified, state.NotificationID)
	return err
}

// MarkReadLocally updates the local read state of a notification.
func (db *DB) MarkReadLocally(ctx context.Context, id string, isRead bool) error {
	_, err := db.ExecContext(ctx, `
		UPDATE orbit_state
		SET is_read_locally = ?
		WHERE notification_id = ?
	`, isRead, id)
	return err
}

// ArchiveThread moves a thread to the archived state.
func (db *DB) ArchiveThread(ctx context.Context, id string) error {
	_, err := db.ExecContext(ctx, `
		UPDATE orbit_state
		SET status = 'archived'
		WHERE notification_id = ?
	`, id)
	return err
}

// UnarchiveThread restores a thread to the tracking state.
func (db *DB) UnarchiveThread(ctx context.Context, id string) error {
	_, err := db.ExecContext(ctx, `
		UPDATE orbit_state
		SET status = 'tracking'
		WHERE notification_id = ?
	`, id)
	return err
}

// MuteThread mutes future alerts for this thread.
func (db *DB) MuteThread(ctx context.Context, id string) error {
	_, err := db.ExecContext(ctx, `
		UPDATE orbit_state
		SET status = 'muted'
		WHERE notification_id = ?
	`, id)
	return err
}

// UnmuteThread unmutes alerts for this thread.
func (db *DB) UnmuteThread(ctx context.Context, id string) error {
	_, err := db.ExecContext(ctx, `
		UPDATE orbit_state
		SET status = 'tracking'
		WHERE notification_id = ?
	`, id)
	return err
}

// SetPriority updates the local priority of a notification.
func (db *DB) SetPriority(ctx context.Context, id string, priority int) error {
	_, err := db.ExecContext(ctx, `
		UPDATE orbit_state
		SET priority = ?
		WHERE notification_id = ?
	`, priority, id)
	return err
}

// MarkNotifiedBatch marks multiple notifications as notified in a single transaction.
func (db *DB) MarkNotifiedBatch(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, "UPDATE orbit_state SET is_notified = TRUE WHERE notification_id = ?")
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	var errs []error
	for _, id := range ids {
		if _, err := stmt.ExecContext(ctx, id); err != nil {
			errs = append(errs, fmt.Errorf("failed to mark notification %s as notified: %w", id, err))
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return tx.Commit()
}

// GetSyncMeta retrieves the sync metadata for a user and endpoint.
func (db *DB) GetSyncMeta(ctx context.Context, userID, key string) (*models.SyncMeta, error) {
	row := db.QueryRowContext(ctx, `
		SELECT user_id, key, last_modified, etag, poll_interval, last_sync_at, last_error, last_error_at
		FROM sync_meta
		WHERE user_id = ? AND key = ?
	`, userID, key)

	var s models.SyncMeta
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
func (db *DB) UpdateSyncMeta(ctx context.Context, s models.SyncMeta) error {
	_, err := db.ExecContext(ctx, `
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

// GetBridgeHealth retrieves the cached health status of the bridge.
func (db *DB) GetBridgeHealth(ctx context.Context) (*models.BridgeHealth, error) {
	row := db.QueryRowContext(ctx, `
		SELECT status, os_version, binary_path, binary_version, updated_at
		FROM bridge_health
		WHERE id = 1
	`)

	var h models.BridgeHealth
	err := row.Scan(&h.Status, &h.OSVersion, &h.BinaryPath, &h.BinaryVersion, &h.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &h, nil
}

// UpdateBridgeHealth updates the cached health status of the bridge.
func (db *DB) UpdateBridgeHealth(ctx context.Context, h models.BridgeHealth) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO bridge_health (id, status, os_version, binary_path, binary_version, updated_at)
		VALUES (1, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			status = excluded.status,
			os_version = excluded.os_version,
			binary_path = excluded.binary_path,
			binary_version = excluded.binary_version,
			updated_at = excluded.updated_at
	`, h.Status, h.OSVersion, h.BinaryPath, h.BinaryVersion, h.UpdatedAt)
	return err
}
