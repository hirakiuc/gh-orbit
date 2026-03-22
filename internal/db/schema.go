package db

// migrations define the versioned schema updates.
var migrations = []string{
	// Version 1: Initial schema
	`CREATE TABLE IF NOT EXISTS schema_version (
		version INTEGER PRIMARY KEY
	);
	CREATE TABLE IF NOT EXISTS notifications (
		github_id TEXT PRIMARY KEY,
		subject_title TEXT NOT NULL,
		subject_type TEXT NOT NULL,
		reason TEXT NOT NULL,
		repository_full_name TEXT NOT NULL,
		html_url TEXT,
		is_enriched BOOLEAN DEFAULT FALSE,
		updated_at DATETIME NOT NULL
	);
	CREATE TABLE IF NOT EXISTS orbit_state (
		notification_id TEXT PRIMARY KEY,
		priority INTEGER DEFAULT 0,
		status TEXT DEFAULT 'entry',
		is_read_locally BOOLEAN DEFAULT FALSE,
		FOREIGN KEY (notification_id) REFERENCES notifications(github_id) ON DELETE CASCADE
	);`,
	// Version 2: Sync metadata for multi-account differential sync
	`CREATE TABLE IF NOT EXISTS sync_meta (
		user_id TEXT NOT NULL,
		key TEXT NOT NULL,
		last_modified TEXT,
		etag TEXT,
		poll_interval INTEGER DEFAULT 60,
		last_sync_at DATETIME,
		last_error TEXT,
		last_error_at DATETIME,
		PRIMARY KEY (user_id, key)
	);`,
	// Version 3: Add subject_url for robust PR number extraction
	`ALTER TABLE notifications ADD COLUMN subject_url TEXT;`,
	// Version 4: Add body and author_login for detail view
	`ALTER TABLE notifications ADD COLUMN body TEXT DEFAULT '';
	 ALTER TABLE notifications ADD COLUMN author_login TEXT DEFAULT '';`,
	// Version 5: Add resource_state for live status (Open, Merged, etc.)
	`ALTER TABLE notifications ADD COLUMN resource_state TEXT DEFAULT '';`,
	// Version 6: Add subject_node_id for GraphQL batch fetching
	`ALTER TABLE notifications ADD COLUMN subject_node_id TEXT DEFAULT '';
	 CREATE INDEX IF NOT EXISTS idx_notifications_subject_node_id ON notifications(subject_node_id);`,
	// Version 7: Add enriched_at for cache expiration logic
	`ALTER TABLE notifications ADD COLUMN enriched_at DATETIME;`,
	// Version 8: Add is_notified to track native alerts and support throttling
	`ALTER TABLE orbit_state ADD COLUMN is_notified BOOLEAN DEFAULT FALSE;`,
	// Version 9: Add bridge_health table for diagnostic caching
	`CREATE TABLE IF NOT EXISTS bridge_health (
		id INTEGER PRIMARY KEY CHECK (id = 1), -- Singleton record
		status TEXT NOT NULL,
		os_version TEXT NOT NULL,
		binary_path TEXT NOT NULL,
		binary_version TEXT NOT NULL,
		updated_at DATETIME NOT NULL
	);`,
	// Version 10: Add review_decision for PR approval status
	`ALTER TABLE notifications ADD COLUMN review_decision TEXT DEFAULT '';`,
	// Version 11: Generalize review_decision to resource_sub_state
	`ALTER TABLE notifications ADD COLUMN resource_sub_state TEXT DEFAULT '';
	 UPDATE notifications SET resource_sub_state = review_decision;`,
}
