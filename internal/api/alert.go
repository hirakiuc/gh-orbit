package api

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/db"
)

// Notifier defines the interface for delivering system notifications.
type Notifier interface {
	Notify(title, subtitle, body, url string, priority int) error
}

// AlertService coordinates the logic for when and how to send system alerts.
type AlertService struct {
	config   *config.Config
	db       *db.DB
	logger   *slog.Logger
	notifier Notifier

	// Per-sync state for throttling
	mu             sync.Mutex
	isInitializing bool
	syncAlertCount int
	syncRepoCounts map[string]int
}

func NewAlertService(ctx context.Context, cfg *config.Config, database *db.DB, logger *slog.Logger) *AlertService {
	return &AlertService{
		config:         cfg,
		db:             database,
		logger:         logger,
		notifier:       NewPlatformNotifier(ctx, logger),
		syncRepoCounts: make(map[string]int),
	}
}

// SyncStart prepares the service for a new synchronization cycle.
// It detects if this is the "Silent Initial Baseline" (empty database).
func (a *AlertService) SyncStart() {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Reset counters
	a.syncAlertCount = 0
	a.syncRepoCounts = make(map[string]int)

	// Check if DB is empty to trigger Silent Baseline
	notifs, err := a.db.ListNotifications()
	a.isInitializing = (err == nil && len(notifs) == 0)

	if a.isInitializing {
		a.logger.Info("alert service: silent initialization baseline active (quiet mode)")
	}
}

// Notify sends a system alert for a GitHub notification if it matches filters and throttling rules.
func (a *AlertService) Notify(n GHNotification) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.config.Notifications.Enabled || a.config.Notifications.Mute {
		return nil
	}

	// 1. Silent Initial Baseline: Never alert during first-ever sync
	if a.isInitializing {
		return nil
	}

	// 2. Filter by reason
	if !a.shouldNotifyReason(n.Reason) {
		return nil
	}

	// 3. Filter by ignored repositories
	for _, repo := range a.config.Notifications.IgnoreRepos {
		if repo == n.Repository.FullName {
			return nil
		}
	}

	// 4. Intelligent Throttling
	a.syncAlertCount++
	repoName := n.Repository.FullName
	a.syncRepoCounts[repoName]++

	// Limit to 5 individual alerts per sync cycle to prevent distraction/crashes
	if a.syncAlertCount > 5 {
		if a.syncAlertCount == 6 {
			// Show a single summary alert once we cross the threshold
			summary := "Multiple new notifications received. Check the TUI for details."
			return a.notifier.Notify("New Notifications", "gh-orbit", summary, "", 1)
		}
		// Silently suppress subsequent items in this cycle
		return nil
	}

	title := n.Subject.Title
	subtitle := repoName
	body := fmt.Sprintf("Reason: %s", n.Reason)
	
	// Determine priority using heuristics
	importance := a.calculateImportance(n)

	a.logger.Debug("sending system alert", 
		"title", title, 
		"importance", importance,
	)

	return a.notifier.Notify(title, subtitle, body, "", importance)
}

func (a *AlertService) calculateImportance(n GHNotification) int {
	// 1. High priority reasons (Level 2 - Time Sensitive)
	switch n.Reason {
	case "mention", "review_requested", "security_alert":
		return 2
	}

	// 2. Triage History (If we already know this thread is high priority)
	state, err := a.db.GetNotification(n.ID)
	if err == nil && state != nil && state.Priority >= 2 {
		return 2
	}

	// Default to Level 1 (Active)
	return 1
}

func (a *AlertService) shouldNotifyReason(reason string) bool {
	if len(a.config.Notifications.Reasons) == 0 {
		return true // Notify for all reasons if whitelist is empty
	}

	for _, r := range a.config.Notifications.Reasons {
		if r == reason {
			return true
		}
	}
	return false
}
