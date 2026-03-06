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

// Notifier defines the interface for delivering system notifications.
type Notifier interface {
	Notify(title, subtitle, body, url string, priority int) error
	Shutdown()
	Status() BridgeStatus
	Warmup() // Proactive health check
	Ready() <-chan struct{}
}

// AlertService coordinates the logic for when and how to send system alerts.
type AlertService struct {
	ctx      context.Context
	config   *config.Config
	db       *db.DB
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

func NewAlertService(ctx context.Context, cfg *config.Config, database *db.DB, logger *slog.Logger) *AlertService {
	return &AlertService{
		ctx:            ctx,
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
func (a *AlertService) SyncStart() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.syncAlertCount = 0
	a.syncRepoCounts = make(map[string]int)

	notifs, err := a.db.ListNotifications()
	a.isInitializing = (err == nil && len(notifs) == 0)

	if a.isInitializing {
		a.logger.InfoContext(a.ctx, "alert service: silent initialization baseline active")
	}
}

// Notify sends a system alert using the best available notifier.
func (a *AlertService) Notify(n GHNotification) error {
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
			return a.getNotifier().Notify("New Notifications", "gh-orbit", "Multiple new notifications received.", "", 1)
		}
		return nil
	}

	title := n.Subject.Title
	subtitle := n.Repository.FullName
	body := fmt.Sprintf("Reason: %s", n.Reason)
	importance := a.calculateImportance(n)

	a.logger.DebugContext(a.ctx, "sending system alert",
		"title", title,
		"importance", importance,
	)

	return a.getNotifier().Notify(title, subtitle, body, "", importance)
}

func (a *AlertService) getNotifier() Notifier {
	// If native is healthy, use it. Otherwise, use fallback.
	if a.native != nil && a.native.Status() == StatusHealthy {
		return a.native
	}
	return a.fallback
}

func (a *AlertService) calculateImportance(n GHNotification) int {
	switch n.Reason {
	case "mention", "review_requested", "security_alert":
		return 2
	}
	state, err := a.db.GetNotification(n.ID)
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
func (a *AlertService) Shutdown() {
	if a.native != nil {
		a.native.Shutdown()
	}
	if a.fallback != nil {
		a.fallback.Shutdown()
	}
}

// BridgeStatus returns the functional state of the primary native bridge.
func (a *AlertService) BridgeStatus() BridgeStatus {
	if a.native == nil {
		return StatusUnsupported
	}
	return a.native.Status()
}

// ProbeAndCacheBridge performs a deep probe of the native bridge and caches the result.
func (a *AlertService) ProbeAndCacheBridge(version string) {
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
	cached, err := a.db.GetBridgeHealth()
	if err == nil && cached != nil {
		if cached.OSVersion == osVersion && cached.BinaryPath == execPath && cached.BinaryVersion == version {
			a.logger.DebugContext(a.ctx, "alert service: using cached bridge status", "status", cached.Status)
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
	_ = a.db.UpdateBridgeHealth(db.BridgeHealth{
		Status:        string(status),
		OSVersion:     osVersion,
		BinaryPath:    execPath,
		BinaryVersion: version,
		UpdatedAt:     time.Now(),
	})

	a.logger.InfoContext(a.ctx, "alert service: bridge probe complete", "status", status)
}
