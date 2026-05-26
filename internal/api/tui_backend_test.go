package api

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/mocks"
	"github.com/hirakiuc/gh-orbit/internal/models"
	"github.com/hirakiuc/gh-orbit/internal/triage"
	"github.com/hirakiuc/gh-orbit/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestAppBackend_Shutdown_DoesNotOwnSharedServices(t *testing.T) {
	mockRepo := mocks.NewMockRepository(t)
	mockSyncer := mocks.NewMockSyncer(t)
	mockEnricher := mocks.NewMockEnricher(t)

	appBackend, err := NewAppBackend(
		"user-1",
		mockRepo,
		mockSyncer,
		mockEnricher,
		nil,
		nil,
		nil,
		nil,
	)
	require.NoError(t, err)

	appBackend.Shutdown(context.Background())

	mockSyncer.AssertNotCalled(t, "Shutdown", mock.Anything)
	mockEnricher.AssertNotCalled(t, "Shutdown", mock.Anything)
}

func TestAppBackend_MarkReadPublishesNotificationsChanged(t *testing.T) {
	mockRepo := mocks.NewMockRepository(t)
	mockSyncer := mocks.NewMockSyncer(t)
	mockEnricher := mocks.NewMockEnricher(t)
	mockClient := mocks.NewMockClient(t)
	snapshot := []triage.NotificationWithState{{Notification: triage.Notification{GitHubID: "notif-1"}, State: triage.State{IsReadLocally: true}}}

	mockRepo.EXPECT().ListNotifications(mock.Anything).Return([]triage.NotificationWithState{{Notification: triage.Notification{GitHubID: "notif-1"}}}, nil).Once()
	mockRepo.EXPECT().MarkReadLocally(mock.Anything, "notif-1", true).Return(nil).Once()
	mockClient.EXPECT().MarkThreadAsRead(mock.Anything, "notif-1").Return(nil).Once()
	mockRepo.EXPECT().ListNotifications(mock.Anything).Return(snapshot, nil).Once()

	published := 0
	appBackend, err := NewAppBackend(
		"user-1",
		mockRepo,
		mockSyncer,
		mockEnricher,
		mockClient,
		nil,
		func() { published++ },
		nil,
	)
	require.NoError(t, err)

	result, err := appBackend.MarkRead(context.Background(), "notif-1", true)
	require.NoError(t, err)
	assert.Equal(t, 1, published)
	assert.Equal(t, types.MarkReadSuccess, result.Status)
	assert.Equal(t, snapshot, result.Notifications)
}

func TestAppBackend_SetPriorityPublishesNotificationsChanged(t *testing.T) {
	mockRepo := mocks.NewMockRepository(t)
	mockSyncer := mocks.NewMockSyncer(t)
	mockEnricher := mocks.NewMockEnricher(t)
	snapshot := []triage.NotificationWithState{{Notification: triage.Notification{GitHubID: "notif-2"}, State: triage.State{Priority: 3}}}

	mockRepo.EXPECT().ListNotifications(mock.Anything).Return([]triage.NotificationWithState{{Notification: triage.Notification{GitHubID: "notif-2"}}}, nil).Once()
	mockRepo.EXPECT().SetPriority(mock.Anything, "notif-2", 3).Return(nil).Once()
	mockRepo.EXPECT().ListNotifications(mock.Anything).Return(snapshot, nil).Once()

	published := 0
	appBackend, err := NewAppBackend(
		"user-1",
		mockRepo,
		mockSyncer,
		mockEnricher,
		nil,
		nil,
		func() { published++ },
		nil,
	)
	require.NoError(t, err)

	result, err := appBackend.SetPriority(context.Background(), "notif-2", 3)
	require.NoError(t, err)
	assert.Equal(t, 1, published)
	assert.Equal(t, types.PriorityUpdateSuccess, result.Status)
	assert.Equal(t, snapshot, result.Notifications)
	assert.Equal(t, "Priority set to High", result.Toast)
}

func TestAppBackend_PersistFetchedDetailPublishesEnrichmentUpdated(t *testing.T) {
	mockRepo := mocks.NewMockRepository(t)
	mockSyncer := mocks.NewMockSyncer(t)
	mockEnricher := mocks.NewMockEnricher(t)

	res := models.EnrichmentResult{
		SubjectNodeID: "node-1",
		Body:          "body",
		Author:        "octocat",
		HTMLURL:       "https://github.com/o/r/pull/1",
	}
	mockEnricher.EXPECT().
		PersistFetchedDetail(mock.Anything, "notif-3", "https://api.github.com/repos/o/r/pulls/1", res).
		Return(nil).
		Once()

	published := 0
	appBackend, err := NewAppBackend(
		"user-1",
		mockRepo,
		mockSyncer,
		mockEnricher,
		nil,
		nil,
		nil,
		func() { published++ },
	)
	require.NoError(t, err)

	require.NoError(t, appBackend.PersistFetchedDetail(context.Background(), "notif-3", "https://api.github.com/repos/o/r/pulls/1", res))
	assert.Equal(t, 1, published)
}

func TestAppBackend_PersistFetchedDetailDoesNotDoublePublishViaEnricherHook(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mockClient := mocks.NewMockClient(t)
	mockRepo := mocks.NewMockEnrichmentRepository(t)

	engine, err := NewEnrichmentEngine(ctx, EnrichParams{
		Client: mockClient,
		DB:     mockRepo,
		Config: config.DefaultConfig(),
		Logger: logger,
	})
	require.NoError(t, err)
	t.Cleanup(func() { engine.Shutdown(ctx) })

	res := models.EnrichmentResult{SubjectNodeID: "node-1", Body: "body"}
	mockRepo.EXPECT().
		EnrichNotification(mock.Anything, "notif-4", "node-1", "body", "", "", "", "").
		Return(nil).
		Once()

	published := 0
	engine.OnMutation = func() { published++ }

	require.NoError(t, engine.PersistFetchedDetail(ctx, "notif-4", "https://api.github.com/repos/o/r/pulls/4", res))
	assert.Equal(t, 0, published)
}
