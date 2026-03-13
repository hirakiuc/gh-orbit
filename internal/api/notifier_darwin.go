package api

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/hirakiuc/gh-orbit/internal/types"
)

// NewPlatformNotifier returns the native macOS notifier.
func NewPlatformNotifier(ctx context.Context, executor types.CommandExecutor, logger *slog.Logger) types.Notifier {
	return NewDarwinNotifier(executor, logger)
}

// DarwinNotifier implements native macOS notifications via osascript.
type DarwinNotifier struct {
	executor types.CommandExecutor
	logger   *slog.Logger
	status   types.BridgeStatus
}

func NewDarwinNotifier(executor types.CommandExecutor, logger *slog.Logger) *DarwinNotifier {
	return &DarwinNotifier{
		executor: executor,
		logger:   logger,
		status:   types.StatusHealthy,
	}
}

func (n *DarwinNotifier) Notify(ctx context.Context, title, subtitle, body, url string, priority int) error {
	script := fmt.Sprintf(`display notification "%s" with title "%s" subtitle "%s"`,
		escapeAppleScript(body),
		escapeAppleScript(title),
		escapeAppleScript(subtitle),
	)

	return n.executor.Run(ctx, "osascript", "-e", script)
}

func (n *DarwinNotifier) Shutdown(ctx context.Context) {
	n.logger.DebugContext(ctx, "darwin notifier shutdown complete")
}

func (n *DarwinNotifier) Status() types.BridgeStatus {
	return n.status
}

// CheckFocusMode detects if "Do Not Disturb" or other focus modes are active.
func CheckFocusMode(executor types.CommandExecutor) string {
	// macOS Sequoia/Sonoma: uses 'dnd -status' via osascript (approximated)
	out, err := executor.Execute(context.Background(), "defaults", "read", "com.apple.controlcenter", "NSStatusItem Visible FocusModes")
	if err != nil {
		return "Unknown"
	}
	if strings.TrimSpace(string(out)) == "1" {
		return "Active"
	}
	return "Inactive"
}

func escapeAppleScript(s string) string {
	return strings.ReplaceAll(s, `"`, `\"`)
}
