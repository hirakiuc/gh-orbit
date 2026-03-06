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

func NewSyncEngine(ctx context.Context, fetcher Fetcher, database *db.DB, alerts *AlertService, logger *slog.Logger) *SyncEngine {
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

// Shutdown ensures all background services are stopped.
func (s *SyncEngine) Shutdown() {
	if s.alerts != nil {
		s.alerts.Shutdown()
	}
}

// BridgeStatus returns the functional state of the alert bridge.
func (s *SyncEngine) BridgeStatus() BridgeStatus {
	if s.alerts == nil {
		return StatusUnknown
	}
	return s.alerts.BridgeStatus()
}

// Sync performs a full synchronization cycle for notifications.
// If force is true, it bypasses the PollInterval check.
// It returns the remaining rate limit if known.
func (s *SyncEngine) Sync(ctx context.Context, userID string, force bool) (int, error) {
	// Prepare alert service for a new cycle (detects Silent Initial Baseline)
	if s.alerts != nil {
		s.alerts.SyncStart()
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

	notifications, newMeta, remaining, err := s.fetcher.FetchNotifications(ctx, meta, force)
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

		var newlyDiscoveredIDs []string

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

			// We only trigger alerts for notifications arriving AFTER the established baseline
			// AND that we haven't notified for yet.
			state, err := s.db.GetNotification(n.ID)
			if err == nil && state != nil && !state.IsNotified {
				// Alerts are sent only for truly new items (arrival > baseline sync)
				if n.UpdatedAt.After(meta.LastSyncAt) {
					if s.alerts != nil {
						_ = s.alerts.Notify(n)
					}
				}
				// Mark as "processed" even if alert failed (to prevent infinite retry storm)
				newlyDiscoveredIDs = append(newlyDiscoveredIDs, n.ID)
			}
		}

		// Batch mark as notified to preserve baseline state
		if len(newlyDiscoveredIDs) > 0 {
			if s.logger.Enabled(ctx, slog.LevelDebug) {
				s.logger.Debug("sync: marking notifications as notified", "count", len(newlyDiscoveredIDs))
			}
			if err := s.db.MarkNotifiedBatch(newlyDiscoveredIDs); err != nil {
				s.logger.Error("failed to mark notifications as notified", "error", err)
			}
		}
	}

	newMeta.LastSyncAt = time.Now()
	newMeta.LastError = "" // Clear previous error on success
	s.logger.Info("notification sync complete", "user_id", userID, "sync_id", syncID)
	return remaining, s.db.UpdateSyncMeta(*newMeta)
}
