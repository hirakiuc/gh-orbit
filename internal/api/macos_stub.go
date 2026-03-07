//go:build !darwin

package api

import (
	"context"
	"log/slog"
)

// NewPlatformNotifier returns a no-op notifier for non-macOS platforms.
// The AlertService already handles tiered fallbacks, so this just needs to satisfy the constructor.
func NewPlatformNotifier(ctx context.Context, logger *slog.Logger) Notifier {
	return &stubNotifier{}
}

type stubNotifier struct{}

func (s *stubNotifier) Notify(ctx context.Context, title, subtitle, body, url string, priority int) error {
	return nil
}

func (s *stubNotifier) Shutdown(ctx context.Context) {}

func (s *stubNotifier) Warmup() {}

func (s *stubNotifier) Ready() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func (s *stubNotifier) Status() BridgeStatus {
	return StatusUnsupported
}

// CheckFocusMode returns a no-op status for non-macOS platforms.
func CheckFocusMode() string {
	return "Unsupported platform"
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
