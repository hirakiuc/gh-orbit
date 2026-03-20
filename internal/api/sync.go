package api

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/github"
	"github.com/hirakiuc/gh-orbit/internal/models"
	"github.com/hirakiuc/gh-orbit/internal/triage"
	"github.com/hirakiuc/gh-orbit/internal/types"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const DefaultPollInterval = 60 // seconds

// SyncEngine orchestrates the synchronization of notifications.
type SyncEngine struct {
	fetcher github.Fetcher
	db      types.SyncRepository
	alerts  Alerter
	logger  *slog.Logger
}

func NewSyncEngine(fetcher github.Fetcher, database types.SyncRepository, alerts Alerter, logger *slog.Logger) *SyncEngine {
	return &SyncEngine{
		fetcher: fetcher,
		db:      database,
		alerts:  alerts,
		logger:  logger,
	}
}

// Fetcher returns the underlying Fetcher instance.
func (s *SyncEngine) Fetcher() github.Fetcher {
	return s.fetcher
}

// Shutdown ensures all background services are stopped gracefully.
func (s *SyncEngine) Shutdown(ctx context.Context) {
	if s.alerts != nil {
		s.alerts.Shutdown(ctx)
	}
	s.logger.DebugContext(ctx, "sync engine shutdown complete")
}

// BridgeStatus returns the functional state of the alert bridge.
func (s *SyncEngine) BridgeStatus() types.BridgeStatus {
	if s.alerts == nil {
		return types.StatusUnknown
	}
	return s.alerts.BridgeStatus()
}

// Sync executes a single synchronization cycle.
func (s *SyncEngine) Sync(ctx context.Context, userID string, force bool) (models.RateLimitInfo, error) {
	// Prepare alert service for a new cycle (detects Silent Initial Baseline)
	if s.alerts != nil {
		s.alerts.SyncStart(ctx)
	}

	tracer := config.GetTracer()
	ctx, span := tracer.Start(ctx, "sync.notifications",
		trace.WithAttributes(
			attribute.String("user_id", userID),
			attribute.Bool("force", force),
		),
	)
	defer span.End()

	syncID := time.Now().UnixNano()
	s.logger.InfoContext(ctx, "starting notification sync",
		"user_id", userID,
		"force", force,
		"sync_id", syncID)

	metaKey := "notifications"
	rlInfo := models.RateLimitInfo{Limit: 5000, Remaining: 5000}

	meta, err := s.db.GetSyncMeta(ctx, userID, metaKey)
	if err != nil {
		return rlInfo, err
	}

	// Initialize meta if not exists
	if meta == nil {
		meta = &models.SyncMeta{
			UserID:       userID,
			Key:          metaKey,
			PollInterval: DefaultPollInterval,
		}
	}

	// 1. Self-Healing: Detect and clear corrupted ETags (e.g., W/"")
	if meta.ETag == `W/""` {
		if s.logger.Enabled(ctx, slog.LevelDebug) {
			s.logger.DebugContext(ctx, "sync: self-healing corrupted ETag", "sync_id", syncID, "etag", meta.ETag)
		}
		meta.ETag = ""
	}

	// Check if we should poll based on LastSyncAt and PollInterval
	if !force && time.Since(meta.LastSyncAt).Seconds() < float64(meta.PollInterval) {
		if s.logger.Enabled(ctx, slog.LevelDebug) {
			s.logger.DebugContext(ctx, "sync: skipping poll, interval not reached",
				"sync_id", syncID,
				"interval", meta.PollInterval,
				"last_sync", meta.LastSyncAt)
		}
		return rlInfo, types.ErrSyncIntervalNotReached // Too soon to poll
	}

	if s.logger.Enabled(ctx, slog.LevelDebug) {
		s.logger.DebugContext(ctx, "sync: executing API fetch",
			"sync_id", syncID,
			"etag", meta.ETag,
			"last_modified", meta.LastModified)
	}

	notifications, newMeta, rlInfo, err := s.fetcher.FetchNotifications(ctx, meta, force)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to fetch notifications", "sync_id", syncID, "error", err)
		meta.LastError = err.Error()
		meta.LastErrorAt = time.Now()
		if updateErr := s.db.UpdateSyncMeta(ctx, *meta); updateErr != nil {
			s.logger.ErrorContext(ctx, "failed to update sync meta after fetch error", "sync_id", syncID, "error", updateErr)
		}
		return rlInfo, err
	}

	span.SetAttributes(attribute.Int("notification_count", len(notifications)))

	// If 304 Not Modified, notifications will be empty but newMeta might have updated PollInterval
	if len(notifications) == 0 {
		if s.logger.Enabled(ctx, slog.LevelDebug) {
			s.logger.DebugContext(ctx, "sync: no new notifications (304 or empty)", "sync_id", syncID)
		}
	} else {
		s.logger.InfoContext(ctx, "sync: processing new notifications",
			"sync_id", syncID,
			"count", len(notifications))

		var newlyDiscoveredIDs []string
		var triageNotifications []triage.Notification
		for _, n := range notifications {
			triageNotifications = append(triageNotifications, triage.Notification{
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
		}

		if err := s.db.UpsertNotifications(ctx, triageNotifications); err != nil {
			s.logger.ErrorContext(ctx, "sync: failed to batch upsert notifications", "error", err)
			return rlInfo, err
		}

		for _, n := range notifications {
			// We only trigger alerts for notifications arriving AFTER the established baseline
			// AND that we haven't notified for yet.
			state, err := s.db.GetNotification(ctx, n.ID)
			if err == nil && state != nil && !state.IsNotified {
				// Alerts are sent only for truly new items (arrival > baseline sync)
				if n.UpdatedAt.After(meta.LastSyncAt) {
					if s.alerts != nil {
						if notifyErr := s.alerts.Notify(ctx, n); notifyErr != nil {
							s.logger.WarnContext(ctx, "failed to send alert for notification", "id", n.ID, "error", notifyErr)
						}
					}
				}
				// Mark as "processed" even if alert failed (to prevent infinite retry storm)
				newlyDiscoveredIDs = append(newlyDiscoveredIDs, n.ID)
			}
		}

		// Batch mark as notified to preserve baseline state
		if len(newlyDiscoveredIDs) > 0 {
			if s.logger.Enabled(ctx, slog.LevelDebug) {
				s.logger.DebugContext(ctx, "sync: marking notifications as notified", "count", len(newlyDiscoveredIDs))
			}
			if err := s.db.MarkNotifiedBatch(ctx, newlyDiscoveredIDs); err != nil {
				s.logger.ErrorContext(ctx, "failed to mark notifications as notified", "error", err)
			}
		}
	}

	newMeta.LastSyncAt = time.Now()
	newMeta.LastError = "" // Clear previous error on success
	s.logger.InfoContext(ctx, "notification sync complete", "user_id", userID, "sync_id", syncID)

	if err := s.db.UpdateSyncMeta(ctx, *newMeta); err != nil {
		s.logger.ErrorContext(ctx, "failed to update sync meta at end of cycle", "sync_id", syncID, "error", err)
		return rlInfo, fmt.Errorf("sync meta update failed: %w", err)
	}

	return rlInfo, nil
}
