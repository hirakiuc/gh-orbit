//go:build darwin

package api

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

var appleScriptReplacer = strings.NewReplacer(
	"\\", "\\\\",
	"\"", "\\\"",
)

type macosNotifier struct {
	logger   *slog.Logger
	executor CommandExecutor
}

// NewPlatformNotifier returns a robust macOS notifier using osascript.
func NewPlatformNotifier(ctx context.Context, executor CommandExecutor, logger *slog.Logger) Notifier {
	return &macosNotifier{
		logger:   logger,
		executor: executor,
	}
}

func (m *macosNotifier) Notify(ctx context.Context, title, subtitle, body, url string, priority int) error {
	// Security: Escape all user-provided strings to prevent AppleScript injection.
	safeTitle := appleScriptReplacer.Replace(title)
	safeSubtitle := appleScriptReplacer.Replace(subtitle)
	safeBody := appleScriptReplacer.Replace(body)

	// Use System Events for reliable background delivery
	script := fmt.Sprintf(
		"tell application \"System Events\" to display notification \"%s\" with title \"%s\" subtitle \"%s\" sound name \"Glass\"",
		safeBody,
		safeTitle,
		safeSubtitle,
	)

	// Fire-and-Forget: Spawning a goroutine ensures AlertService is never blocked by osascript.
	go func() {
		// Use request context (WithoutCancel) to ensure delivery even if the main loop advances,
		// but tie it to the system-level timeout.
		cmdCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()

		if err := m.executor.Run(cmdCtx, "osascript", "-e", script); err != nil {
			m.logger.WarnContext(cmdCtx, "macos: notification delivery failed", "error", err)
		}
	}()

	return nil
}

func (m *macosNotifier) Shutdown(ctx context.Context) {}

func (m *macosNotifier) Status() BridgeStatus {
	return StatusHealthy
}

// CheckFocusMode performs a soft-failure probe for active macOS Focus modes.
func CheckFocusMode(executor CommandExecutor) string {
	// NSStatusItem Visible FocusModes is a reliable indicator in modern macOS
	out, err := executor.Execute(context.Background(), "defaults", "read", "com.apple.controlcenter", "NSStatusItem Visible FocusModes")
	if err != nil {
		return "Unknown (Permissions restricted)"
	}
	
	val := strings.TrimSpace(string(out))
	if val == "1" {
		return "Active (Notifications may be suppressed)"
	}
	return "Inactive"
}
