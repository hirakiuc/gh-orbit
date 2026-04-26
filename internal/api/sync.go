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
	fetcher    github.Fetcher
	db         types.SyncRepository
	alerts     Alerter
	logger     *slog.Logger
	OnMutation func()
}

func NewSyncEngine(p SyncParams) (*SyncEngine, error) {
	if p.Fetcher == nil {
		return nil, fmt.Errorf("fetcher is required for SyncEngine")
	}
	if p.DB == nil {
		return nil, fmt.Errorf("database is required for SyncEngine")
	}
	if p.Logger == nil {
		return nil, fmt.Errorf("logger is required for SyncEngine")
	}

	return &SyncEngine{
		fetcher:    p.Fetcher,
		db:         p.DB,
		alerts:     p.Alerts,
		logger:     p.Logger,
		OnMutation: func() {}, // Default no-op
	}, nil
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

	meta, err := s.initSyncMeta(ctx, userID, metaKey, syncID)
	if err != nil {
		return rlInfo, err
	}

	if s.shouldSkipPoll(ctx, meta, force, syncID) {
		return rlInfo, types.ErrSyncIntervalNotReached
	}

	notifications, newMeta, rlInfo, err := s.fetcher.FetchNotifications(ctx, meta, force)
	if err != nil {
		return s.handleFetchError(ctx, meta, syncID, rlInfo, err)
	}

	span.SetAttributes(attribute.Int("notification_count", len(notifications)))

	if err := s.processSyncResults(ctx, notifications, meta, syncID); err != nil {
		return rlInfo, err
	}

	newMeta.LastSyncAt = time.Now()
	newMeta.LastError = "" // Clear previous error on success
	s.logger.InfoContext(ctx, "notification sync complete", "user_id", userID, "sync_id", syncID)

	if err := s.db.UpdateSyncMeta(ctx, *newMeta); err != nil {
		s.logger.ErrorContext(ctx, "failed to update sync meta at end of cycle", "sync_id", syncID, "error", err)
		return rlInfo, fmt.Errorf("sync meta update failed: %w", err)
	}

	s.OnMutation()

	return rlInfo, nil
}

func (s *SyncEngine) initSyncMeta(ctx context.Context, userID string, key string, syncID int64) (*models.SyncMeta, error) {
	meta, err := s.db.GetSyncMeta(ctx, userID, key)
	if err != nil {
		return nil, err
	}

	// Initialize meta if not exists
	if meta == nil {
		meta = &models.SyncMeta{
			UserID:       userID,
			Key:          key,
			PollInterval: DefaultPollInterval,
		}
	}

	// Self-Healing: Detect and clear corrupted ETags (e.g., W/"")
	if meta.ETag == `W/""` {
		if s.logger.Enabled(ctx, slog.LevelDebug) {
			s.logger.DebugContext(ctx, "sync: self-healing corrupted ETag", "sync_id", syncID, "etag", meta.ETag)
		}
		meta.ETag = ""
	}

	return meta, nil
}

func (s *SyncEngine) shouldSkipPoll(ctx context.Context, meta *models.SyncMeta, force bool, syncID int64) bool {
	if force {
		return false
	}

	if time.Since(meta.LastSyncAt).Seconds() < float64(meta.PollInterval) {
		if s.logger.Enabled(ctx, slog.LevelDebug) {
			s.logger.DebugContext(ctx, "sync: skipping poll, interval not reached",
				"sync_id", syncID,
				"interval", meta.PollInterval,
				"last_sync", meta.LastSyncAt)
		}
		return true
	}
	return false
}

func (s *SyncEngine) handleFetchError(ctx context.Context, meta *models.SyncMeta, syncID int64, rl models.RateLimitInfo, err error) (models.RateLimitInfo, error) {
	s.logger.ErrorContext(ctx, "failed to fetch notifications", "sync_id", syncID, "error", err)
	meta.LastError = err.Error()
	meta.LastErrorAt = time.Now()
	if updateErr := s.db.UpdateSyncMeta(ctx, *meta); updateErr != nil {
		s.logger.ErrorContext(ctx, "failed to update sync meta after fetch error", "sync_id", syncID, "error", updateErr)
	}
	return rl, err
}

func (s *SyncEngine) processSyncResults(ctx context.Context, notifications []github.Notification, meta *models.SyncMeta, syncID int64) error {
	// If 304 Not Modified, notifications will be empty
	if len(notifications) == 0 {
		if s.logger.Enabled(ctx, slog.LevelDebug) {
			s.logger.DebugContext(ctx, "sync: no new notifications (304 or empty)", "sync_id", syncID)
		}
		return nil
	}

	s.logger.InfoContext(ctx, "sync: processing new notifications",
		"sync_id", syncID,
		"count", len(notifications))

	var triageNotifications []triage.Notification
	for _, n := range notifications {
		triageNotifications = append(triageNotifications, triage.Notification{
			GitHubID:           n.ID,
			SubjectTitle:       n.Subject.Title,
			SubjectURL:         n.Subject.URL,
			SubjectType:        triage.SubjectType(n.Subject.Type),
			Reason:             n.Reason,
			RepositoryFullName: n.Repository.FullName,
			SubjectNodeID:      n.Subject.NodeID,
			HTMLURL:            "", // Will be enriched in later phases if needed
			UpdatedAt:          n.UpdatedAt,
		})
	}

	if err := s.db.UpsertNotifications(ctx, triageNotifications); err != nil {
		s.logger.ErrorContext(ctx, "sync: failed to batch upsert notifications", "error", err)
		return err
	}

	return s.triggerAlerts(ctx, notifications, meta.LastSyncAt, syncID)
}

func (s *SyncEngine) triggerAlerts(ctx context.Context, notifications []github.Notification, lastSyncAt time.Time, syncID int64) error {
	var newlyDiscoveredIDs []string

	for _, n := range notifications {
		if s.shouldTriggerAlert(ctx, n, lastSyncAt) {
			s.sendAlert(ctx, n)
			newlyDiscoveredIDs = append(newlyDiscoveredIDs, n.ID)
		}
	}

	return s.markAsNotified(ctx, newlyDiscoveredIDs)
}

func (s *SyncEngine) shouldTriggerAlert(ctx context.Context, n github.Notification, lastSyncAt time.Time) bool {
	// We only trigger alerts for notifications arriving AFTER the established baseline
	// AND that we haven't notified for yet.
	state, err := s.db.GetNotification(ctx, n.ID)
	if err != nil || state == nil || state.IsNotified {
		return false
	}

	// Alerts are sent only for truly new items (arrival > baseline sync)
	return n.UpdatedAt.After(lastSyncAt)
}

func (s *SyncEngine) sendAlert(ctx context.Context, n github.Notification) {
	if s.alerts == nil {
		return
	}

	if err := s.alerts.Notify(ctx, n); err != nil {
		s.logger.WarnContext(ctx, "failed to send alert for notification", "id", n.ID, "error", err)
	}
}

func (s *SyncEngine) markAsNotified(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	if s.logger.Enabled(ctx, slog.LevelDebug) {
		s.logger.DebugContext(ctx, "sync: marking notifications as notified", "count", len(ids))
	}

	if err := s.db.MarkNotifiedBatch(ctx, ids); err != nil {
		s.logger.ErrorContext(ctx, "failed to mark notifications as notified", "error", err)
		return err
	}

	return nil
}
