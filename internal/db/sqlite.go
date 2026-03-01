package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// DB represents the database connection pool.
type DB struct {
	*sql.DB
}

// Open opens a connection to the SQLite database.
func Open() (*DB, error) {
	dbPath, err := resolveDBPath()
	if err != nil {
		return nil, err
	}

	// Create parent directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return nil, fmt.Errorf("failed to create db directory: %w", err)
	}

	// modernc.org/sqlite driver
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	instance := &DB{db}
	if err := instance.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}

	return instance, nil
}

// resolveDBPath follows the XDG Base Directory specification.
func resolveDBPath() (string, error) {
	// Respect XDG_DATA_HOME if set, otherwise use os.UserConfigDir as a reliable base
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		// On macOS: ~/Library/Application Support/gh-orbit/
		// On Linux: ~/.local/share/gh-orbit/
		if os.Getenv("XDG_DATA_HOME") == "" {
			// Fallback to platform-specific data dir
			dataHome = filepath.Join(home, ".local", "share")
		}
	}

	return filepath.Join(dataHome, "gh-orbit", "orbit.db"), nil
}

// Close closes the database connection.
func (db *DB) Close() error {
	return db.DB.Close()
}
