package db

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestUpsertAndGetNotification(t *testing.T) {
	logger := slog.Default()
	ctx := context.Background()
	db, err := OpenInMemory(ctx, logger)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	notif := Notification{
		GitHubID:           "123",
		SubjectTitle:       "Test PR",
		SubjectURL:         "https://api.github.com/repos/owner/repo/pulls/1",
		SubjectType:        "PullRequest",
		Reason:             "mention",
		RepositoryFullName: "owner/repo",
		UpdatedAt:          time.Now(),
	}

	err = db.UpsertNotification(ctx, notif)
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

func TestUpsertPreservesLocalState(t *testing.T) {
	logger := slog.Default()
	ctx := context.Background()
	db, err := OpenInMemory(ctx, logger)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	id := "123"
	notif := Notification{
		GitHubID:  id,
		UpdatedAt: time.Now(),
	}

	require.NoError(t, db.UpsertNotification(ctx, notif))

	// Manually set some triage state
	err = db.UpdateOrbitState(ctx, OrbitState{
		NotificationID: id,
		Priority:       3,
		Status:         "archived",
		IsReadLocally:  true,
	})
	require.NoError(t, err)

	// Upsert again (as if from a new poll)
	require.NoError(t, db.UpsertNotification(ctx, notif))

	// Verify triage state was NOT overwritten
	ns, err := db.GetNotification(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, 3, ns.Priority)
	assert.Equal(t, "archived", ns.Status)
	assert.True(t, ns.IsReadLocally)
}

func TestMarkNotifiedBatch(t *testing.T) {
	logger := slog.Default()
	ctx := context.Background()
	db, err := OpenInMemory(ctx, logger)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ids := []string{"1", "2", "3"}
	for _, id := range ids {
		require.NoError(t, db.UpsertNotification(ctx, Notification{
			GitHubID:  id,
			UpdatedAt: time.Now(),
		}))
	}

	// Batch mark
	require.NoError(t, db.MarkNotifiedBatch(ctx, ids))

	// Verify all are marked
	for _, id := range ids {
		ns, err := db.GetNotification(ctx, id)
		require.NoError(t, err)
		assert.True(t, ns.IsNotified, "Expected notification %s to be marked as notified", id)
	}
}
