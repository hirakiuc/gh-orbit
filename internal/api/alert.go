package api

import (
	"fmt"

	"github.com/gen2brain/beeep"
	"github.com/hirakiuc/gh-orbit/internal/config"
)

// AlertService handles system notifications.
type AlertService struct {
	config *config.Config
}

func NewAlertService(cfg *config.Config) *AlertService {
	return &AlertService{config: cfg}
}

// Notify sends a system alert for a GitHub notification if it matches filters.
func (a *AlertService) Notify(n GHNotification) error {
	if !a.config.Notifications.Enabled || a.config.Notifications.Mute {
		return nil
	}

	// Filter by reason
	if !a.shouldNotifyReason(n.Reason) {
		return nil
	}

	// Filter by ignored repositories
	for _, repo := range a.config.Notifications.IgnoreRepos {
		if repo == n.Repository.FullName {
			return nil
		}
	}

	title := fmt.Sprintf("[%s] %s", n.Repository.FullName, n.Subject.Title)
	message := fmt.Sprintf("Reason: %s", n.Reason)

	// beeep.Notify(title, message, icon)
	// We pass an empty string for the icon as per feedback on macOS limitations.
	return beeep.Notify(title, message, "")
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
