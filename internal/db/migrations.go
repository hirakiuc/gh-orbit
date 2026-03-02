package db

import (
	"database/sql"
	"fmt"
)

// migrate applies versioned schema updates.
func (db *DB) migrate() error {
	// 1. Create schema_version if not exists
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY)`)
	if err != nil {
		return fmt.Errorf("failed to create schema_version table: %w", err)
	}

	// 2. Get current version
	var currentVersion int
	err = db.QueryRow("SELECT MAX(version) FROM schema_version").Scan(&currentVersion)
	if err == sql.ErrNoRows || (err == nil && currentVersion == 0) {
		currentVersion = 0
	} else if err != nil {
		// If the table is empty but MAX returns NULL, it might not be ErrNoRows
		// in some sqlite drivers, but Scan into int might fail or give 0.
		// modernc.org/sqlite usually handles this fine.
		currentVersion = 0
	}

	// 3. Apply missing migrations
	for i := currentVersion; i < len(migrations); i++ {
		version := i + 1
		tx, err := db.Begin()
		if err != nil {
			return err
		}

		if _, err := tx.Exec(migrations[i]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration to version %d failed: %w", version, err)
		}

		// Update schema version: ensure only one row remains
		if _, err := tx.Exec("DELETE FROM schema_version"); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("failed to clear schema_version: %w", err)
		}
		if _, err := tx.Exec("INSERT INTO schema_version (version) VALUES (?)", version); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("failed to update schema version to %d: %w", version, err)
		}

		if err := tx.Commit(); err != nil {
			return err
		}
	}

	return nil
}
