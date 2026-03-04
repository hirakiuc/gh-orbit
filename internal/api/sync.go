package api

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/db"
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
// It returns the remaining rate limit if known.
func (s *SyncEngine) Sync(userID string) (int, error) {
	s.logger.Info("starting notification sync", "user_id", userID)
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

	// Check if we should poll based on LastSyncAt and PollInterval
	if time.Since(meta.LastSyncAt).Seconds() < float64(meta.PollInterval) {
		s.logger.Debug("skipping sync, poll interval not reached", "interval", meta.PollInterval)
		return remaining, nil // Too soon to poll
	}

	notifications, newMeta, remaining, err := s.fetcher.FetchNotifications(meta)
	if err != nil {
		s.logger.Error("failed to fetch notifications", "error", err)
		meta.LastError = err.Error()
		meta.LastErrorAt = time.Now()
		_ = s.db.UpdateSyncMeta(*meta)
		return remaining, err
	}

	// If 304 Not Modified, notifications will be empty but newMeta might have updated PollInterval
	if len(notifications) > 0 {
		for _, n := range notifications {
			err := s.db.UpsertNotification(db.Notification{
				GitHubID:           n.ID,
				SubjectTitle:       n.Subject.Title,
				SubjectURL:         n.Subject.URL,
				SubjectType:        n.Subject.Type,
				Reason:             n.Reason,
				RepositoryFullName: n.Repository.FullName,
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
	s.logger.Info("notification sync complete", "user_id", userID)
	return remaining, s.db.UpdateSyncMeta(*newMeta)
}
