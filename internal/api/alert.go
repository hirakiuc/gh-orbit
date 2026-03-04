package api

import (
	"fmt"
	"log/slog"

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
}

func NewAlertService(cfg *config.Config, database *db.DB, logger *slog.Logger) *AlertService {
	return &AlertService{
		config:   cfg,
		db:       database,
		logger:   logger,
		notifier: NewPlatformNotifier(logger),
	}
}

// Notify sends a system alert for a GitHub notification if it matches filters.
func (a *AlertService) Notify(n GHNotification) error {
	if !a.config.Notifications.Enabled || a.config.Notifications.Mute {
		a.logger.Debug("skipping alert, notifications disabled or muted")
		return nil
	}

	// Filter by reason
	if !a.shouldNotifyReason(n.Reason) {
		a.logger.Debug("skipping alert, reason not in whitelist", "reason", n.Reason)
		return nil
	}

	// Filter by ignored repositories
	for _, repo := range a.config.Notifications.IgnoreRepos {
		if repo == n.Repository.FullName {
			return nil
		}
	}

	title := n.Subject.Title
	subtitle := n.Repository.FullName
	body := fmt.Sprintf("Reason: %s", n.Reason)
	
	// Determine priority using heuristics
	importance := a.calculateImportance(n)

	a.logger.Info("sending system alert", 
		"title", title, 
		"importance", importance,
		"reason", n.Reason,
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

	// 3. User Config (Future: could add critical_repos to YAML)

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
