package api

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/db"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const DefaultPollInterval = 60 // seconds

// SyncEngine orchestrates the synchronization of notifications.
type SyncEngine struct {
	fetcher Fetcher
	db      *db.DB
	alerts  *AlertService
	logger  *slog.Logger
}

func NewSyncEngine(fetcher Fetcher, database *db.DB, alerts *AlertService, logger *slog.Logger) *SyncEngine {
	return &SyncEngine{
		fetcher: fetcher,
		db:      database,
		alerts:  alerts,
		logger:  logger,
	}
}

// Fetcher returns the underlying Fetcher instance.
func (s *SyncEngine) Fetcher() Fetcher {
	return s.fetcher
}

// Sync performs a full synchronization cycle for notifications.
// If force is true, it bypasses the PollInterval check.
// It returns the remaining rate limit if known.
func (s *SyncEngine) Sync(ctx context.Context, userID string, force bool) (int, error) {
	tracer := config.GetTracer()
	ctx, span := tracer.Start(ctx, "sync.notifications",
		trace.WithAttributes(
			attribute.String("user_id", userID),
			attribute.Bool("force", force),
		),
	)
	defer span.End()

	syncID := time.Now().UnixNano()
	s.logger.Info("starting notification sync", 
		"user_id", userID, 
		"force", force, 
		"sync_id", syncID)
	
	metaKey := "notifications"
	remaining := 5000 // Default

	meta, err := s.db.GetSyncMeta(userID, metaKey)
	if err != nil {
		return remaining, err
	}

	// Initialize meta if not exists
	if meta == nil {
		meta = &db.SyncMeta{
			UserID:       userID,
			Key:          metaKey,
			PollInterval: DefaultPollInterval,
		}
	}

	// 1. Self-Healing: Detect and clear corrupted ETags (e.g., W/"")
	if meta.ETag == `W/""` {
		if s.logger.Enabled(ctx, slog.LevelDebug) {
			s.logger.Debug("sync: self-healing corrupted ETag", "sync_id", syncID, "etag", meta.ETag)
		}
		meta.ETag = ""
	}

	// Check if we should poll based on LastSyncAt and PollInterval
	if !force && time.Since(meta.LastSyncAt).Seconds() < float64(meta.PollInterval) {
		if s.logger.Enabled(ctx, slog.LevelDebug) {
			s.logger.Debug("sync: skipping poll, interval not reached", 
				"sync_id", syncID, 
				"interval", meta.PollInterval,
				"last_sync", meta.LastSyncAt)
		}
		return remaining, nil // Too soon to poll
	}

	if s.logger.Enabled(ctx, slog.LevelDebug) {
		s.logger.Debug("sync: executing API fetch", 
			"sync_id", syncID, 
			"etag", meta.ETag, 
			"last_modified", meta.LastModified)
	}

	notifications, newMeta, remaining, err := s.fetcher.FetchNotifications(meta, force)
	if err != nil {
		s.logger.Error("failed to fetch notifications", "sync_id", syncID, "error", err)
		meta.LastError = err.Error()
		meta.LastErrorAt = time.Now()
		_ = s.db.UpdateSyncMeta(*meta)
		return remaining, err
	}

	span.SetAttributes(attribute.Int("notification_count", len(notifications)))

	// If 304 Not Modified, notifications will be empty but newMeta might have updated PollInterval
	if len(notifications) == 0 {
		if s.logger.Enabled(ctx, slog.LevelDebug) {
			s.logger.Debug("sync: no new notifications (304 or empty)", "sync_id", syncID)
		}
	} else {
		s.logger.Info("sync: processing new notifications", 
			"sync_id", syncID, 
			"count", len(notifications))

		for _, n := range notifications {
			err := s.db.UpsertNotification(db.Notification{
				GitHubID:           n.ID,
				SubjectTitle:       n.Subject.Title,
				SubjectURL:         n.Subject.URL,
				SubjectType:        n.Subject.Type,
				Reason:             n.Reason,
				RepositoryFullName: n.Repository.FullName,
				SubjectNodeID:      n.Subject.NodeID,
				HTMLURL:            "", // Will be enriched in later phases if needed
				UpdatedAt:          n.UpdatedAt,
			})
			if err != nil {
				return remaining, fmt.Errorf("failed to save notification %s: %w", n.ID, err)
			}

			// Trigger system alert for new notifications
			if s.alerts != nil && n.UpdatedAt.After(meta.LastSyncAt) {
				_ = s.alerts.Notify(n)
			}
		}
	}

	newMeta.LastSyncAt = time.Now()
	newMeta.LastError = "" // Clear previous error on success
	s.logger.Info("notification sync complete", "user_id", userID, "sync_id", syncID)
	return remaining, s.db.UpdateSyncMeta(*newMeta)
}
