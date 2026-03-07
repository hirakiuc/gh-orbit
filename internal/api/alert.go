package api

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/db"
)

// AlertService coordinates the logic for when and how to send system alerts.
type AlertService struct {
	config   *config.Config
	db       AlertRepository
	logger   *slog.Logger
	
	// Tiered Notifiers
	native   Notifier // Tier 1: Direct macOS bridge
	fallback Notifier // Tier 2: Cross-platform fallback (beeep)

	// Per-sync state for throttling
	mu             sync.Mutex
	isInitializing bool
	syncAlertCount int
	syncRepoCounts map[string]int
}

func NewAlertService(ctx context.Context, cfg *config.Config, database AlertRepository, logger *slog.Logger) *AlertService {
	return &AlertService{
		config:         cfg,
		db:             database,
		logger:         logger,
		native:         NewPlatformNotifier(ctx, logger),
		fallback:       NewBeeepNotifier(logger),
		syncRepoCounts: make(map[string]int),
	}
}

// Warmup proactively assesses the bridge health without firing an alert.
func (a *AlertService) Warmup() {
	if a.native != nil {
		a.native.Warmup()
	}
	if a.fallback != nil {
		a.fallback.Warmup()
	}
}

// Ready returns a channel that closes when the primary notification tier is ready.
func (a *AlertService) Ready() <-chan struct{} {
	if a.native != nil {
		return a.native.Ready()
	}
	// If no native bridge, fallback is usually instant
	ch := make(chan struct{})
	close(ch)
	return ch
}

// SyncStart prepares the service for a new synchronization cycle.
func (a *AlertService) SyncStart(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.syncAlertCount = 0
	a.syncRepoCounts = make(map[string]int)

	notifs, err := a.db.ListNotifications(ctx)
	a.isInitializing = (err == nil && len(notifs) == 0)

	if a.isInitializing {
		a.logger.InfoContext(ctx, "alert service: silent initialization baseline active")
	}
}

// Notify sends a system alert using the best available notifier.
func (a *AlertService) Notify(ctx context.Context, n GHNotification) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.config.Notifications.Enabled || a.config.Notifications.Mute || a.isInitializing {
		return nil
	}

	if !a.shouldNotifyReason(n.Reason) {
		return nil
	}

	for _, repo := range a.config.Notifications.IgnoreRepos {
		if repo == n.Repository.FullName {
			return nil
		}
	}

	// Throttling
	a.syncAlertCount++
	if a.syncAlertCount > 5 {
		if a.syncAlertCount == 6 {
			return a.getNotifier().Notify(ctx, "New Notifications", "gh-orbit", "Multiple new notifications received.", "", 1)
		}
		return nil
	}

	title := n.Subject.Title
	subtitle := n.Repository.FullName
	body := fmt.Sprintf("Reason: %s", n.Reason)
	importance := a.calculateImportance(ctx, n)

	a.logger.DebugContext(ctx, "sending system alert",
		"title", title,
		"importance", importance,
	)

	return a.getNotifier().Notify(ctx, title, subtitle, body, "", importance)
}

// TestNotify sends a diagnostic alert bypassing the silent startup baseline.
func (a *AlertService) TestNotify(ctx context.Context, title, subtitle, body string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Bypass isInitializing and config checks for diagnostics
	a.logger.InfoContext(ctx, "sending diagnostic system alert", "tier", a.getTierName())
	return a.getNotifier().Notify(ctx, title, subtitle, body, "", 1)
}

// ActiveTierInfo returns the name and status of the current active notification tier.
func (a *AlertService) ActiveTierInfo() (string, BridgeStatus) {
	n := a.getNotifier()
	name := a.getTierName()
	return name, n.Status()
}

func (a *AlertService) getTierName() string {
	if a.native != nil && a.native.Status() == StatusHealthy {
		return "Native Bridge"
	}
	// On macOS, if native fails, we use AppleScript fallback via the macosNotifier's internal logic.
	if runtime.GOOS == "darwin" && a.native != nil {
		if a.native.Status() == StatusUnsupported || a.native.Status() == StatusBroken {
			return "AppleScript Fallback"
		}
	}
	return "Beeep (Cross-Platform)"
}

func (a *AlertService) getNotifier() Notifier {
	// If native is healthy, use it. Otherwise, use fallback.
	if a.native != nil && a.native.Status() == StatusHealthy {
		return a.native
	}
	// macosNotifier also has its own internal AppleScript fallback if it's not healthy.
	if runtime.GOOS == "darwin" && a.native != nil {
		return a.native
	}
	return a.fallback
}

func (a *AlertService) calculateImportance(ctx context.Context, n GHNotification) int {
	switch n.Reason {
	case "mention", "review_requested", "security_alert":
		return 2
	}
	state, err := a.db.GetNotification(ctx, n.ID)
	if err == nil && state != nil && state.Priority >= 2 {
		return 2
	}
	return 1
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

// Shutdown ensures all notification workers are stopped.
func (a *AlertService) Shutdown(ctx context.Context) {
	if a.native != nil {
		a.native.Shutdown(ctx)
	}
	if a.fallback != nil {
		a.fallback.Shutdown(ctx)
	}
	a.logger.DebugContext(ctx, "alert service shutdown complete")
}

// BridgeStatus returns the functional state of the primary native bridge.
func (a *AlertService) BridgeStatus() BridgeStatus {
	if a.native == nil {
		return StatusUnsupported
	}
	return a.native.Status()
}

// ProbeAndCacheBridge performs a deep probe of the native bridge and caches the result.
func (a *AlertService) ProbeAndCacheBridge(ctx context.Context, version string) {
	if runtime.GOOS != "darwin" {
		return
	}

	// Ensure bridge is warmed up (probed) before checking status
	a.Warmup()

	// 1. Get current environment fingerprints
	osVersion := "unknown"
	out, err := exec.Command("sysctl", "-n", "kern.osversion").Output()
	if err == nil {
		osVersion = strings.TrimSpace(string(out))
	}
	execPath, _ := os.Executable()

	// 2. Check cache
	cached, err := a.db.GetBridgeHealth(ctx)
	if err == nil && cached != nil {
		if cached.OSVersion == osVersion && cached.BinaryPath == execPath && cached.BinaryVersion == version {
			a.logger.DebugContext(ctx, "alert service: using cached bridge status", "status", cached.Status)
			return
		}
	}

	// 3. Perform Deep Probe
	probes := ProbeBridge()
	allPassed := true
	for _, p := range probes {
		if !p.Passed {
			allPassed = false
			break
		}
	}

	status := StatusHealthy
	if !allPassed {
		status = StatusBroken
	}

	// 4. Update Cache
	_ = a.db.UpdateBridgeHealth(ctx, db.BridgeHealth{
		Status:        string(status),
		OSVersion:     osVersion,
		BinaryPath:    execPath,
		BinaryVersion: version,
		UpdatedAt:     time.Now(),
	})

	a.logger.InfoContext(ctx, "alert service: bridge probe complete", "status", status)
}
