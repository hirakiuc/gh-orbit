//go:build !darwin

package api

import (
	"context"
	"log/slog"
)

// NewPlatformNotifier returns a no-op notifier for non-macOS platforms.
// The AlertService already handles tiered fallbacks, so this just needs to satisfy the constructor.
func NewPlatformNotifier(ctx context.Context, logger *slog.Logger) Notifier {
	return nil
}

// BridgeProbe represents the result of a single bridge diagnostic check.
type BridgeProbe struct {
	Name    string
	Passed  bool
	Message string
}

// ProbeBridge returns an empty probe list for non-macOS platforms.
func ProbeBridge() []BridgeProbe {
	return nil
}
