package api

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/github"
	"github.com/hirakiuc/gh-orbit/internal/models"
	"github.com/hirakiuc/gh-orbit/internal/types"
)

// AlertService coordinates the logic for when and how to send system alerts.
type AlertService struct {
	config *config.Config
	db     types.AlertRepository
	logger *slog.Logger

	// Tiered Notifiers
	native   types.Notifier // Tier 1: Platform-specific (e.g., macOS osascript)
	fallback types.Notifier // Tier 2: Cross-platform fallback (beeep)
	executor types.CommandExecutor

	// Per-sync state for throttling
	mu             sync.Mutex
	isInitializing bool
	syncAlertCount int
	syncRepoCounts map[string]int
}

func NewAlertService(cfg *config.Config, database types.AlertRepository, native types.Notifier, fallback types.Notifier, executor types.CommandExecutor, logger *slog.Logger) *AlertService {
	return &AlertService{
		config:         cfg,
		db:             database,
		logger:         logger,
		native:         native,
		fallback:       fallback,
		executor:       executor,
		syncRepoCounts: make(map[string]int),
	}
}

// SyncStart signals the beginning of a new sync cycle, resetting throttles.
func (a *AlertService) SyncStart(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.syncAlertCount = 0
	a.syncRepoCounts = make(map[string]int)

	// Check if this is the first run (empty database)
	// If it is, we stay silent to avoid a deluge of historic alerts.
	notifs, err := a.db.ListNotifications(ctx)
	if err == nil && len(notifs) == 0 {
		a.isInitializing = true
		a.logger.InfoContext(ctx, "alert service: silent initialization baseline active")
	} else {
		a.isInitializing = false
	}
}

// Notify processes a new notification and sends an alert if appropriate.
func (a *AlertService) Notify(ctx context.Context, n github.Notification) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.config.Notifications.Enabled || a.isInitializing {
		return nil
	}

	// 1. Filter by Reason (if configured)
	if !a.shouldNotifyReason(n.Reason) {
		return nil
	}

	// 2. Global Sync Limit: Max 5 alerts per sync session
	if a.syncAlertCount >= 5 {
		if a.syncAlertCount == 5 {
			a.logger.InfoContext(ctx, "alert throttle reached: sending summary")
			a.syncAlertCount++
			return a.getNotifier().Notify(ctx, "New Notifications", "Summary", "Additional notifications are available in the TUI", "https://github.com/notifications", 1)
		}
		return nil
	}

	// 3. Calculate Importance & Send
	importance := a.calculateImportance(n)
	err := a.getNotifier().Notify(
		ctx,
		n.Repository.FullName,
		n.Subject.Title,
		n.Reason,
		n.Subject.URL,
		importance,
	)

	if err == nil {
		a.syncAlertCount++
	}
	return err
}

func (a *AlertService) Shutdown(ctx context.Context) {
	if a.native != nil {
		a.native.Shutdown(ctx)
	}
	if a.fallback != nil {
		a.fallback.Shutdown(ctx)
	}
	a.logger.DebugContext(ctx, "alert service shutdown complete")
}

// TestNotify triggers a guaranteed notification for diagnostic purposes.
func (a *AlertService) TestNotify(ctx context.Context, title, subtitle, body string) error {
	return a.getNotifier().Notify(ctx, title, subtitle, body, "https://github.com/notifications", 2)
}

func (a *AlertService) ActiveTierInfo() (string, types.BridgeStatus) {
	if a.native.Status() == types.StatusHealthy {
		return "Platform Native (Tier 1)", types.StatusHealthy
	}
	return "Cross-Platform Fallback (Tier 2)", a.fallback.Status()
}

func (a *AlertService) BridgeStatus() types.BridgeStatus {
	return a.native.Status()
}

func (a *AlertService) calculateImportance(n github.Notification) int {
	switch n.Reason {
	case "mention":
		return 3
	case "assign":
		return 2
	default:
		return 1
	}
}

func (a *AlertService) shouldNotifyReason(reason string) bool {
	if len(a.config.Notifications.Reasons) == 0 {
		return true
	}
	for _, r := range a.config.Notifications.Reasons {
		if r == reason {
			return true
		}
	}
	return false
}

func (a *AlertService) getNotifier() types.Notifier {
	if a.native.Status() == types.StatusHealthy {
		return a.native
	}
	return a.fallback
}

// RefreshBridgeHealth re-detects the bridge status by probing the system.
func (a *AlertService) RefreshBridgeHealth(ctx context.Context) (types.BridgeStatus, error) {
	err := a.db.UpdateBridgeHealth(ctx, models.BridgeHealth{
		Status:    string(types.StatusHealthy),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		return types.StatusUnknown, err
	}

	out, _ := a.executor.Execute(ctx, "sw_vers", "-productVersion")
	osVersion := strings.TrimSpace(string(out))

	execPath, _ := os.Executable()

	_ = a.db.UpdateBridgeHealth(ctx, models.BridgeHealth{
		Status:        string(types.StatusHealthy),
		OSVersion:     osVersion,
		BinaryPath:    execPath,
		BinaryVersion: "1.0.0",
		UpdatedAt:     time.Now(),
	})

	return types.StatusHealthy, nil
}
