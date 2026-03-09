package api

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/config"
)

// AlertService coordinates the logic for when and how to send system alerts.
type AlertService struct {
	config   *config.Config
	db       AlertRepository
	logger   *slog.Logger
	
	// Tiered Notifiers
	native   Notifier // Tier 1: Direct macOS bridge
	fallback Notifier // Tier 2: Cross-platform fallback (beeep)
	executor CommandExecutor

	// Per-sync state for throttling
	mu             sync.Mutex
	isInitializing bool
	syncAlertCount int
	syncRepoCounts map[string]int
}

func NewAlertService(cfg *config.Config, database AlertRepository, native Notifier, fallback Notifier, executor CommandExecutor, logger *slog.Logger) *AlertService {
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

// Warmup proactively assesses the bridge health without firing an alert.
func (a *AlertService) Warmup() {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Proactively probe native tier
	a.native.Warmup()
	// Fallback tier is typically lazy or does not require warmup
}

// Ready returns a channel that is closed when the service is fully initialized.
func (a *AlertService) Ready() <-chan struct{} {
	return a.native.Ready()
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
func (a *AlertService) Notify(ctx context.Context, n GHNotification) error {
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

// TestNotify triggers a guaranteed notification for diagnostic purposes.
func (a *AlertService) TestNotify(ctx context.Context, title, subtitle, body string) error {
	return a.getNotifier().Notify(ctx, title, subtitle, body, "https://github.com/notifications", 2)
}

// ActiveTierInfo returns the name and health of the current primary notifier.
func (a *AlertService) ActiveTierInfo() (string, BridgeStatus) {
	if a.native.Status() == StatusHealthy {
		return "macOS Native (Tier 1)", StatusHealthy
	}
	return "Beeep Fallback (Tier 2)", a.fallback.Status()
}

func (a *AlertService) getNotifier() Notifier {
	if a.native.Status() == StatusHealthy {
		return a.native
	}
	return a.fallback
}

func (a *AlertService) calculateImportance(n GHNotification) int {
	switch n.Reason {
	case "mention":
		return 3 // High
	case "review_requested", "assign":
		return 2 // Medium
	default:
		return 1 // Low
	}
}

func (a *AlertService) shouldNotifyReason(reason string) bool {
	if len(a.config.Notifications.Reasons) == 0 {
		return true // Allow all if not configured
	}
	for _, r := range a.config.Notifications.Reasons {
		if r == reason {
			return true
		}
	}
	return false
}

// Shutdown gracefully terminates all notifier tiers.
func (a *AlertService) Shutdown(ctx context.Context) {
	a.native.Shutdown(ctx)
	a.fallback.Shutdown(ctx)
}

// BridgeStatus reports the aggregate health of the alerting system.
func (a *AlertService) BridgeStatus() BridgeStatus {
	return a.native.Status()
}

// ProbeAndCacheBridge assesses environmental compatibility and caches it locally.
func (a *AlertService) ProbeAndCacheBridge() {
	// Ensure bridge is warmed up (probed) before checking status
	a.Warmup()

	// 1. Get current environment fingerprints
	osVersion := "unknown"
	out, err := a.executor.Execute(context.Background(), "sysctl", "-n", "kern.osversion")
	if err == nil {
		osVersion = strings.TrimSpace(string(out))
	}
	execPath, _ := os.Executable()

	// 2. Cache
	_ = a.db.UpdateBridgeHealth(context.Background(), BridgeHealth{
		Status:        string(a.native.Status()),
		OSVersion:     osVersion,
		BinaryPath:    execPath,
		BinaryVersion: "dev", // TODO: inject version
		UpdatedAt:     time.Now(),
	})
}
