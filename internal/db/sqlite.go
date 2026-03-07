package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"syscall"

	"github.com/hirakiuc/gh-orbit/internal/config"
	_ "modernc.org/sqlite"
)

// DB represents the database connection pool.
type DB struct {
	*sql.DB
	logger *slog.Logger
}

// Open opens a connection to the SQLite database, performing migrations if necessary.
func Open(ctx context.Context, logger *slog.Logger) (*DB, error) {
	primaryDir, err := config.ResolveDataDir()
	if err != nil {
		return nil, err
	}
	primaryPath := filepath.Join(primaryDir, "orbit.db")

	// 1. Perform Discovery and Migration if necessary
	if err := migrateLegacyData(ctx, logger, primaryPath); err != nil {
		logger.ErrorContext(ctx, "persistence migration failed", "error", err)
		// We continue even if migration fails to allow the app to boot, 
		// but it might mean a fresh DB is created.
	}

	// 2. Ensure Primary Directory exists with strict permissions
	if err := config.EnsurePrivateDir(primaryDir); err != nil {
		return nil, fmt.Errorf("failed to secure db directory: %w", err)
	}

	logger.InfoContext(ctx, "opening database", "path", primaryPath)

	// modernc.org/sqlite driver
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)", primaryPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	instance := &DB{db, logger}
	if err := instance.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}

	// 3. Recursive Permission Audit (Self-Healing)
	if err := config.AuditPermissions(ctx, logger, primaryDir); err != nil {
		logger.WarnContext(ctx, "persistence permission audit failed", "error", err)
	}

	return instance, nil
}

// migrateLegacyData implements the Discovery Ladder and Atomic Migration.
func migrateLegacyData(ctx context.Context, logger *slog.Logger, primaryPath string) error {
	if _, err := os.Stat(primaryPath); err == nil {
		return nil // Already in primary location
	}

	// Discovery Ladder
	ladder := []string{
		// Legacy 1: XDG_STATE_HOME (previous version)
		os.Getenv("XDG_STATE_HOME"),
		// Legacy 2: ~/.config/gh/extensions/gh-orbit (old monolithic layout)
		"", 
	}

	var legacyPath string
	for _, base := range ladder {
		var candidate string
		if base != "" {
			candidate = filepath.Join(base, "gh-orbit", "orbit.db")
		} else {
			home, _ := os.UserHomeDir()
			candidate = filepath.Join(home, ".config", "gh", "extensions", "gh-orbit", "orbit.db")
		}

		if _, err := os.Stat(candidate); err == nil { // #nosec G703: Candidate path is internally resolved
			legacyPath = candidate
			break
		}
	}

	if legacyPath == "" {
		return nil // No legacy data found
	}

	logger.InfoContext(ctx, "legacy data discovered, initiating migration", "source", legacyPath, "target", primaryPath)

	// 1. Acquire Global Migration Lock
	lockPath := filepath.Join(os.TempDir(), "gh-orbit-migration.lock")
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304: Path is system temp dir
	if err != nil {
		return fmt.Errorf("failed to open migration lock: %w", err)
	}
	defer func() { _ = lockFile.Close() }()

	// #nosec G115: Fd is guaranteed non-negative for open files
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			logger.DebugContext(ctx, "migration already in progress by another instance")
			return nil
		}
		return err
	}
	// #nosec G115: Fd is guaranteed non-negative
	defer func() { _ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN) }()

	// 2. Atomic Collapse (WAL Checkpoint)
	// We open the legacy DB temporarily to collapse sidecars
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)", legacyPath)
	ldb, err := sql.Open("sqlite", dsn)
	if err == nil {
		_, _ = ldb.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
		_ = ldb.Close()
	}

	// 3. Stage-Swap-Cleanup Migration
	return performAtomicMove(ctx, logger, filepath.Dir(legacyPath), filepath.Dir(primaryPath))
}

func performAtomicMove(ctx context.Context, logger *slog.Logger, srcDir, destDir string) error {
	tmpDest := destDir + ".migrating.tmp"
	_ = os.RemoveAll(tmpDest)

	if err := os.MkdirAll(filepath.Dir(tmpDest), 0o700); err != nil {
		return err
	}

	// Copy data to staging area
	if err := copyDir(srcDir, tmpDest); err != nil {
		return fmt.Errorf("copy failed: %w", err)
	}

	// Deterministic Verification
	srcHash, err := computeDirHash(srcDir)
	if err != nil { return err }
	destHash, err := computeDirHash(tmpDest)
	if err != nil { return err }

	if srcHash != destHash {
		return fmt.Errorf("migration verification failed: hash mismatch")
	}

	// Atomic Swap
	if err := os.MkdirAll(filepath.Dir(destDir), 0o700); err != nil {
		return err
	}
	if err := os.Rename(tmpDest, destDir); err != nil {
		return fmt.Errorf("atomic swap failed: %w", err)
	}

	// Cleanup Legacy
	logger.InfoContext(ctx, "migration successful, cleaning up legacy artifacts", "path", srcDir)
	return os.RemoveAll(srcDir) // #nosec G703: srcDir is a validated legacy path
}

func copyDir(src, dest string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error { // #nosec G703: src is internally resolved
		if err != nil { return err }
		rel, _ := filepath.Rel(src, path)
		targetPath := filepath.Join(dest, rel)

		if info.IsDir() {
			return os.MkdirAll(targetPath, 0o700)
		}

		return copyFile(path, targetPath)
	})
}

func copyFile(src, dest string) error {
	in, err := os.Open(src) // #nosec G304: Path is validated during walk
	if err != nil { return err }
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY, 0o600) // #nosec G304: dest is internally resolved
	if err != nil { return err }
	defer func() { _ = out.Close() }()

	_, err = io.Copy(out, in)
	return err
}

func computeDirHash(root string) (string, error) {
	h := sha256.New()
	var files []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error { // #nosec G703: root is internally resolved
		if err != nil { return err }
		if !info.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil { return "", err }

	sort.Strings(files)

	for _, f := range files {
		rel, _ := filepath.Rel(root, f)
		h.Write([]byte(rel))
		
		in, err := os.Open(f) // #nosec G304: f is validated during walk
		if err != nil { return "", err }
		_, _ = io.Copy(h, in)
		_ = in.Close()
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// OpenInMemory opens an in-memory SQLite database for testing.
func OpenInMemory(ctx context.Context, logger *slog.Logger) (*DB, error) {
	dsn := "file::memory:?cache=shared&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open in-memory database: %w", err)
	}

	instance := &DB{db, logger}
	if err := instance.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}

	return instance, nil
}

// Close closes the database connection.
func (db *DB) Close() error {
	return db.DB.Close()
}
