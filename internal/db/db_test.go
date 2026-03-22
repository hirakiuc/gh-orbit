package db

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/models"
	"github.com/hirakiuc/gh-orbit/internal/triage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"modernc.org/sqlite"
	_ "modernc.org/sqlite"
)

func TestUpsertAndGetNotification(t *testing.T) {
	logger := slog.Default()
	ctx := context.Background()
	db, err := OpenInMemory(ctx, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	notif := triage.Notification{
		GitHubID:           "123",
		SubjectTitle:       "Test PR",
		SubjectURL:         "https://api.github.com/repos/owner/repo/pulls/1",
		SubjectType:        "PullRequest",
		Reason:             "mention",
		RepositoryFullName: "owner/repo",
		UpdatedAt:          time.Now(),
	}

	err = db.UpsertNotifications(ctx, []triage.Notification{notif})
	require.NoError(t, err)

	// Verify retrieval
	ns, err := db.GetNotification(ctx, "123")
	require.NoError(t, err)
	require.NotNil(t, ns)

	assert.Equal(t, "Test PR", ns.SubjectTitle)
	assert.Equal(t, 0, ns.Priority)
	assert.Equal(t, "entry", ns.Status)
	assert.False(t, ns.IsReadLocally)
}

func TestUpsertNotificationsBatch(t *testing.T) {
	logger := slog.Default()
	ctx := context.Background()
	db, err := OpenInMemory(ctx, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	notifs := []triage.Notification{
		{
			GitHubID:           "1",
			SubjectTitle:       "PR 1",
			SubjectType:        "PullRequest",
			RepositoryFullName: "owner/repo",
			UpdatedAt:          time.Now(),
		},
		{
			GitHubID:           "2",
			SubjectTitle:       "PR 2",
			SubjectType:        "PullRequest",
			RepositoryFullName: "owner/repo",
			UpdatedAt:          time.Now(),
		},
	}

	err = db.UpsertNotifications(ctx, notifs)
	require.NoError(t, err)

	// Verify both exist
	n1, err := db.GetNotification(ctx, "1")
	require.NoError(t, err)
	assert.Equal(t, "PR 1", n1.SubjectTitle)

	n2, err := db.GetNotification(ctx, "2")
	require.NoError(t, err)
	assert.Equal(t, "PR 2", n2.SubjectTitle)
}

func TestUpsertPreservesLocalState(t *testing.T) {
	logger := slog.Default()
	ctx := context.Background()
	db, err := OpenInMemory(ctx, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	id := "123"
	notif := triage.Notification{
		GitHubID:  id,
		UpdatedAt: time.Now(),
	}

	require.NoError(t, db.UpsertNotifications(ctx, []triage.Notification{notif}))

	// Manually set some triage state
	err = db.UpdateOrbitState(ctx, triage.State{
		NotificationID: id,
		Priority:       3,
		Status:         "archived",
		IsReadLocally:  true,
	})
	require.NoError(t, err)

	// Upsert again (as if from a new poll)
	require.NoError(t, db.UpsertNotifications(ctx, []triage.Notification{notif}))

	// Verify triage state was NOT overwritten
	ns, err := db.GetNotification(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, ns)

	assert.Equal(t, 3, ns.Priority)
	assert.Equal(t, "archived", ns.Status)
	assert.True(t, ns.IsReadLocally)
}

func TestMarkNotifiedBatch(t *testing.T) {
	logger := slog.Default()
	ctx := context.Background()
	db, err := OpenInMemory(ctx, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	ids := []string{"1", "2", "3"}
	var batch []triage.Notification
	for _, id := range ids {
		batch = append(batch, triage.Notification{
			GitHubID:  id,
			UpdatedAt: time.Now(),
		})
	}
	require.NoError(t, db.UpsertNotifications(ctx, batch))

	// Batch mark
	require.NoError(t, db.MarkNotifiedBatch(ctx, ids))

	// Verify all are marked
	for _, id := range ids {
		ns, err := db.GetNotification(ctx, id)
		require.NoError(t, err)
		require.NotNil(t, ns)
		assert.True(t, ns.IsNotified, "Expected notification %s to be marked as notified", id)
	}
}

func TestRepository_Actions(t *testing.T) {
	logger := slog.Default()
	ctx := context.Background()
	db, err := OpenInMemory(ctx, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	id := "action-test"
	require.NoError(t, db.UpsertNotifications(ctx, []triage.Notification{{
		GitHubID:  id,
		UpdatedAt: time.Now(),
	}}))

	// 1. Set Priority
	require.NoError(t, db.SetPriority(ctx, id, 2))
	ns, err := db.GetNotification(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, ns)
	assert.Equal(t, 2, ns.Priority)

	// 2. Mark Read/Unread
	require.NoError(t, db.MarkReadLocally(ctx, id, true))
	ns, err = db.GetNotification(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, ns)
	assert.True(t, ns.IsReadLocally)

	require.NoError(t, db.MarkReadLocally(ctx, id, false))
	ns, err = db.GetNotification(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, ns)
	assert.False(t, ns.IsReadLocally)

	// 3. Archive/Unarchive
	require.NoError(t, db.ArchiveThread(ctx, id))
	ns, err = db.GetNotification(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, ns)
	assert.Equal(t, "archived", ns.Status)

	require.NoError(t, db.UnarchiveThread(ctx, id))
	ns, err = db.GetNotification(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, ns)
	assert.Equal(t, "tracking", ns.Status)

	// 4. Mute/Unmute
	require.NoError(t, db.MuteThread(ctx, id))
	ns, err = db.GetNotification(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, ns)
	assert.Equal(t, "muted", ns.Status)

	require.NoError(t, db.UnmuteThread(ctx, id))
	ns, err = db.GetNotification(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, ns)
	assert.Equal(t, "tracking", ns.Status)
}

func TestRepository_MetadataAndEnrichment(t *testing.T) {
	logger := slog.Default()
	ctx := context.Background()
	db, err := OpenInMemory(ctx, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	id := "enrich-test"
	require.NoError(t, db.UpsertNotifications(ctx, []triage.Notification{{
		GitHubID:  id,
		UpdatedAt: time.Now(),
	}}))

	// 1. Enrich Notification (Combined body, author, etc)
	require.NoError(t, db.EnrichNotification(ctx, id, "Some body", "author", "https://github.com/u", "OPEN", "APPROVED"))

	// Verify
	ns, err := db.GetNotification(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, ns)

	assert.Equal(t, "Some body", ns.Body)
	assert.Equal(t, "author", ns.AuthorLogin)
	assert.Equal(t, "https://github.com/u", ns.HTMLURL)
	assert.Equal(t, "OPEN", ns.ResourceState)
	assert.Equal(t, "APPROVED", ns.ResourceSubState)
	assert.True(t, ns.IsEnriched)
}

func TestRepository_SyncAndHealth(t *testing.T) {
	logger := slog.Default()
	ctx := context.Background()
	db, err := OpenInMemory(ctx, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// 1. Sync Meta
	meta := models.SyncMeta{
		UserID: "user-1",
		Key:    "notifications",
		ETag:   "etag-123",
	}
	require.NoError(t, db.UpdateSyncMeta(ctx, meta))

	sm, err := db.GetSyncMeta(ctx, "user-1", "notifications")
	require.NoError(t, err)
	require.NotNil(t, sm)
	assert.Equal(t, "etag-123", sm.ETag)

	// 2. Bridge Health
	health := models.BridgeHealth{
		Status: "active",
	}
	require.NoError(t, db.UpdateBridgeHealth(ctx, health))

	h, err := db.GetBridgeHealth(ctx)
	require.NoError(t, err)
	require.NotNil(t, h)
	assert.Equal(t, "active", h.Status)
}

func TestRepository_Errors(t *testing.T) {
	logger := slog.Default()
	ctx := context.Background()
	db, err := OpenInMemory(ctx, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// 1. Get non-existent: returns nil, nil by design
	ns, err := db.GetNotification(ctx, "missing")
	require.NoError(t, err)
	assert.Nil(t, ns)
}

func TestRepository_ConstraintError(t *testing.T) {
	logger := slog.Default()
	ctx := context.Background()
	db, err := OpenInMemory(ctx, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// Insert once
	_, err = db.Exec(`INSERT INTO notifications
 (github_id, subject_title, subject_type, reason, repository_full_name, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"dup", "title", "Issue", "reason", "repo", time.Now())
	require.NoError(t, err)

	// Insert duplicate ID
	_, err = db.Exec(`INSERT INTO notifications
 (github_id, subject_title, subject_type, reason, repository_full_name, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"dup", "title", "Issue", "reason", "repo", time.Now())

	require.Error(t, err)

	// Use Go 1.26 errors.AsType for SQLite error validation
	if sqliteErr, ok := errors.AsType[*sqlite.Error](err); ok {
		assert.Equal(t, 1555, sqliteErr.Code(), "Expected SQLITE_CONSTRAINT_PRIMARYKEY (1555)")
	} else {
		t.Errorf("Expected *sqlite.Error, got %T", err)
	}
}

func TestRepository_UpdateByNodeID(t *testing.T) {
	logger := slog.Default()
	ctx := context.Background()
	db, err := OpenInMemory(ctx, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	id := "node-test"
	require.NoError(t, db.UpsertNotifications(ctx, []triage.Notification{{
		GitHubID:      id,
		SubjectNodeID: "node-123",
		UpdatedAt:     time.Now(),
	}}))

	// 1. Update Resource State by Node ID
	require.NoError(t, db.UpdateResourceStateByNodeID(ctx, "node-123", "MERGED", "APPROVED"))
	ns, err := db.GetNotification(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, ns)
	assert.Equal(t, "MERGED", ns.ResourceState)
	assert.Equal(t, "APPROVED", ns.ResourceSubState)

	// 2. Update Subject Node ID
	require.NoError(t, db.UpdateSubjectNodeID(ctx, id, "node-456"))
	ns, err = db.GetNotification(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, ns)
	assert.Equal(t, "node-456", ns.SubjectNodeID)
}

func TestMigration_AtomicMove(t *testing.T) {
	logger := slog.Default()
	ctx := context.Background()

	srcDir := t.TempDir()
	destDir := t.TempDir() + "/target"

	// 1. Create dummy data
	fPath := srcDir + "/test.db"
	require.NoError(t, os.WriteFile(fPath, []byte("sqlite-data"), 0o600))

	// 2. Perform atomic move
	err := performAtomicMove(ctx, logger, srcDir, destDir)
	require.NoError(t, err)

	// 3. Verify
	_, err = os.Stat(destDir + "/test.db")
	assert.NoError(t, err, "File should exist in destination")

	_, err = os.Stat(srcDir)
	assert.True(t, os.IsNotExist(err), "Source directory should be cleaned up")
}

func TestMigration_ExistingDest(t *testing.T) {
	logger := slog.Default()
	ctx := context.Background()

	srcDir := t.TempDir()
	destDir := t.TempDir() + "/target"

	// 1. Create dummy data in source
	fPath := srcDir + "/test.db"
	require.NoError(t, os.WriteFile(fPath, []byte("sqlite-data"), 0o600))

	// 2. Simulate doctor bug: create an empty destination directory
	require.NoError(t, os.MkdirAll(destDir, 0o700))

	// 3. Perform atomic move
	err := performAtomicMove(ctx, logger, srcDir, destDir)
	require.NoError(t, err, "Migration should succeed even if destDir exists")

	// 4. Verify
	_, err = os.Stat(destDir + "/test.db")
	assert.NoError(t, err, "File should exist in destination")

	_, err = os.Stat(srcDir)
	assert.True(t, os.IsNotExist(err), "Source directory should be cleaned up")

	// Ensure no backup directories are left behind (approximately)
	entries, err := os.ReadDir(filepath.Dir(destDir))
	require.NoError(t, err)
	for _, entry := range entries {
		assert.False(t, entry.IsDir() && strings.HasPrefix(entry.Name(), "target.bak."), "Backup directory should be cleaned up: %s", entry.Name())
	}
}

func TestMigration_HashAndCopy(t *testing.T) {
	// Test computeDirHash and copyDir
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(dir+"/f1", []byte("data"), 0o600))

	h1, err := computeDirHash(dir)
	require.NoError(t, err)
	require.NotEmpty(t, h1)

	dest := t.TempDir() + "/copy"
	require.NoError(t, copyDir(dir, dest))

	h2, err := computeDirHash(dest)
	require.NoError(t, err)
	assert.Equal(t, h1, h2)
}

func TestOpen_Success(t *testing.T) {
	ctx := context.Background()
	logger := slog.Default()

	// Mock userHome and XDG env for temp isolation
	tmpHome := t.TempDir()
	originalUserHome := userHome
	userHome = func() (string, error) { return tmpHome, nil }
	t.Cleanup(func() { userHome = originalUserHome })

	t.Setenv("XDG_DATA_HOME", tmpHome)

	// Test Open
	db, err := Open(ctx, logger)
	require.NoError(t, err)
	require.NotNil(t, db)
	t.Cleanup(func() { _ = db.Close() })
}

func TestListNotifications(t *testing.T) {
	logger := slog.Default()
	ctx := context.Background()
	db, err := OpenInMemory(ctx, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	require.NoError(t, db.UpsertNotifications(ctx, []triage.Notification{{GitHubID: "1", UpdatedAt: time.Now()}}))
	require.NoError(t, db.UpsertNotifications(ctx, []triage.Notification{{GitHubID: "2", UpdatedAt: time.Now()}}))

	list, err := db.ListNotifications(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 2)
}
