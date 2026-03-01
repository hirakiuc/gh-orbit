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
	err = db.QueryRow("SELECT version FROM schema_version").Scan(&currentVersion)
	if err == sql.ErrNoRows {
		currentVersion = 0
	} else if err != nil {
		return fmt.Errorf("failed to get current schema version: %w", err)
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

		if currentVersion == 0 {
			_, err = tx.Exec("INSERT INTO schema_version (version) VALUES (?)", version)
		} else {
			_, err = tx.Exec("UPDATE schema_version SET version = ?", version)
		}

		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("failed to update schema version to %d: %w", version, err)
		}

		if err := tx.Commit(); err != nil {
			return err
		}
	}

	return nil
}
