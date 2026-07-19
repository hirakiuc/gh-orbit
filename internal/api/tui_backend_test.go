package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
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

	appBackend, err := NewAppBackend(AppBackendParams{
		UserID:   "user-1",
		Store:    mockRepo,
		Syncer:   mockSyncer,
		Enricher: mockEnricher,
	})
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
	snapshot := []triage.NotificationWithState{{Notification: triage.Notification{GitHubID: "notif-1"}, State: triage.State{IsReadLocally: true, IsHandledLocally: true}}}

	mockRepo.EXPECT().ListNotifications(mock.Anything).Return([]triage.NotificationWithState{{Notification: triage.Notification{GitHubID: "notif-1"}}}, nil).Once()
	mockRepo.EXPECT().MarkReadLocally(mock.Anything, "notif-1", true).Return(nil).Once()
	mockClient.EXPECT().MarkThreadAsRead(mock.Anything, "notif-1").Return(nil).Once()
	mockRepo.EXPECT().ListNotifications(mock.Anything).Return(snapshot, nil).Once()

	published := 0
	appBackend, err := NewAppBackend(AppBackendParams{
		UserID:                      "user-1",
		Store:                       mockRepo,
		Client:                      mockClient,
		Syncer:                      mockSyncer,
		Enricher:                    mockEnricher,
		PublishNotificationsChanged: func() { published++ },
	})
	require.NoError(t, err)

	result, err := appBackend.MarkRead(context.Background(), "notif-1", true)
	require.NoError(t, err)
	assert.Equal(t, 1, published)
	assert.Equal(t, types.MarkReadSuccess, result.Status)
	assert.Equal(t, snapshot, result.Notifications)
}

func TestAppBackend_ApplyNotificationBatchPublishesOnceAndReportsPartialRemoteFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	traffic := NewAPITrafficController(ctx, slog.Default())
	t.Cleanup(func() { traffic.Shutdown(context.Background()) })

	mockRepo := mocks.NewMockRepository(t)
	mockSyncer := mocks.NewMockSyncer(t)
	mockEnricher := mocks.NewMockEnricher(t)
	mockClient := mocks.NewMockClient(t)
	before := []triage.NotificationWithState{{Notification: triage.Notification{GitHubID: "a"}}, {Notification: triage.Notification{GitHubID: "b"}}}
	after := applyNotificationBatchFallback(before, types.NotificationBatchRequest{Operation: types.NotificationBatchRead, IDs: []string{"a", "b"}})
	mockRepo.EXPECT().ListNotifications(mock.Anything).Return(before, nil).Once()
	mockRepo.EXPECT().ApplyNotificationBatchLocally(mock.Anything, types.NotificationBatchRequest{Operation: types.NotificationBatchRead, IDs: []string{"a", "b"}}).Return(nil).Once()
	mockClient.EXPECT().MarkThreadAsRead(mock.Anything, "a").Return(nil).Once()
	mockClient.EXPECT().MarkThreadAsRead(mock.Anything, "b").Return(errors.New("remote unavailable")).Once()
	mockRepo.EXPECT().ListNotifications(mock.Anything).Return(after, nil).Once()

	var published atomic.Int32
	backend, err := NewAppBackend(AppBackendParams{
		UserID: "user", Store: mockRepo, Client: mockClient, Syncer: mockSyncer, Enricher: mockEnricher,
		BatchExecutor: traffic, PublishNotificationsChanged: func() { published.Add(1) },
	})
	require.NoError(t, err)
	result, err := backend.ApplyNotificationBatch(ctx, types.NotificationBatchRequest{Operation: types.NotificationBatchRead, IDs: []string{"b", "a"}})
	require.NoError(t, err)
	assert.Equal(t, types.NotificationBatchCommitted, result.Status)
	assert.Equal(t, int32(1), published.Load())
	require.Len(t, result.Outcomes, 2)
	assert.Equal(t, types.NotificationRemoteSucceeded, result.Outcomes[0].Status)
	assert.Equal(t, types.NotificationRemoteFailed, result.Outcomes[1].Status)
}

func TestAppBackend_IndependentReadAndHandledMutations(t *testing.T) {
	mockRepo := mocks.NewMockRepository(t)
	mockSyncer := mocks.NewMockSyncer(t)
	mockEnricher := mocks.NewMockEnricher(t)
	mockClient := mocks.NewMockClient(t)
	before := []triage.NotificationWithState{{Notification: triage.Notification{GitHubID: "notif"}, State: triage.State{IsHandledLocally: true}}}
	afterRead := []triage.NotificationWithState{{Notification: triage.Notification{GitHubID: "notif"}, State: triage.State{IsReadLocally: true, IsHandledLocally: true}}}
	afterHandled := []triage.NotificationWithState{{Notification: triage.Notification{GitHubID: "notif"}, State: triage.State{IsReadLocally: true}}}

	mockRepo.EXPECT().ListNotifications(mock.Anything).Return(before, nil).Once()
	mockRepo.EXPECT().SetReadLocally(mock.Anything, "notif", true).Return(nil).Once()
	mockClient.EXPECT().MarkThreadAsRead(mock.Anything, "notif").Return(nil).Once()
	mockRepo.EXPECT().ListNotifications(mock.Anything).Return(afterRead, nil).Once()
	mockRepo.EXPECT().ListNotifications(mock.Anything).Return(afterRead, nil).Once()
	mockRepo.EXPECT().SetHandledLocally(mock.Anything, "notif", false).Return(nil).Once()
	mockRepo.EXPECT().ListNotifications(mock.Anything).Return(afterHandled, nil).Once()

	published := 0
	backend, err := NewAppBackend(AppBackendParams{
		UserID: "user", Store: mockRepo, Client: mockClient, Syncer: mockSyncer, Enricher: mockEnricher,
		PublishNotificationsChanged: func() { published++ },
	})
	require.NoError(t, err)

	readResult, err := backend.SetRead(context.Background(), "notif", true)
	require.NoError(t, err)
	assert.True(t, readResult.Notifications[0].IsHandledLocally)
	handledResult, err := backend.SetHandled(context.Background(), "notif", false)
	require.NoError(t, err)
	assert.True(t, handledResult.Notifications[0].IsReadLocally)
	assert.False(t, handledResult.Notifications[0].IsHandledLocally)
	assert.Equal(t, 2, published)
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
	appBackend, err := NewAppBackend(AppBackendParams{
		UserID:                      "user-1",
		Store:                       mockRepo,
		Syncer:                      mockSyncer,
		Enricher:                    mockEnricher,
		PublishNotificationsChanged: func() { published++ },
	})
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
	appBackend, err := NewAppBackend(AppBackendParams{
		UserID:                   "user-1",
		Store:                    mockRepo,
		Syncer:                   mockSyncer,
		Enricher:                 mockEnricher,
		PublishEnrichmentUpdated: func() { published++ },
	})
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
