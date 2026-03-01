package db

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// DB represents the database connection pool.
type DB struct {
	*sql.DB
	logger *slog.Logger
}

// Open opens a connection to the SQLite database.
func Open(logger *slog.Logger) (*DB, error) {
	dbPath, err := resolveDBPath()
	if err != nil {
		return nil, err
	}

	logger.Info("opening database", "path", dbPath)

	// Create parent directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, fmt.Errorf("failed to create db directory: %w", err)
	}

	// modernc.org/sqlite driver
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	instance := &DB{db, logger}
	if err := instance.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}

	return instance, nil
}

// resolveDBPath follows the XDG Base Directory specification.
func resolveDBPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(configDir, "gh-orbit", "orbit.db"), nil
}

// Close closes the database connection.
func (db *DB) Close() error {
	return db.DB.Close()
}
