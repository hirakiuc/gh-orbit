package api

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/github"
	"github.com/hirakiuc/gh-orbit/internal/mocks"
	"github.com/hirakiuc/gh-orbit/internal/models"
	"github.com/hirakiuc/gh-orbit/internal/triage"
	"github.com/stretchr/testify/mock"
)

func BenchmarkSyncEngine_Sync(b *testing.B) {
	logger := slog.New(slog.NewJSONHandler(ioDiscard{}, nil))
	ctx := context.Background()

	mockFetcher := mocks.NewMockFetcher(b)
	mockRepo := mocks.NewMockSyncRepository(b)
	mockAlerter := mocks.NewMockAlerter(b)

	// Pre-create many notifications
	notifs := make([]github.Notification, 1000)
	for i := 0; i < 1000; i++ {
		notifs[i] = github.Notification{ID: "notif-id", UpdatedAt: time.Now()}
	}

	engine := NewSyncEngine(mockFetcher, mockRepo, mockAlerter, logger)

	b.ResetTimer()
	for b.Loop() {
		mockAlerter.EXPECT().SyncStart(mock.Anything).Return().Maybe()
		mockAlerter.EXPECT().Notify(mock.Anything, mock.Anything).Return(nil).Maybe()
		mockRepo.EXPECT().GetSyncMeta(mock.Anything, "user-1", "notifications").Return(nil, nil).Maybe()
		mockFetcher.EXPECT().FetchNotifications(mock.Anything, mock.Anything, true).Return(notifs, &models.SyncMeta{}, models.RateLimitInfo{Limit: 5000, Remaining: 5000}, nil).Maybe()
		mockRepo.EXPECT().UpsertNotifications(mock.Anything, mock.Anything).Return(nil).Maybe()
		mockRepo.EXPECT().GetNotification(mock.Anything, mock.Anything).Return(&triage.NotificationWithState{}, nil).Maybe()
		mockRepo.EXPECT().MarkNotifiedBatch(mock.Anything, mock.Anything).Return(nil).Maybe()
		mockRepo.EXPECT().UpdateSyncMeta(mock.Anything, mock.Anything).Return(nil).Maybe()

		_, _ = engine.Sync(ctx, "user-1", true)
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
