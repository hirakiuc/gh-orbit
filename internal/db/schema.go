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
}
