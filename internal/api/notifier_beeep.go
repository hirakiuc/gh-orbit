package api

import (
	"context"
	"log/slog"

	"github.com/gen2brain/beeep"
	"github.com/hirakiuc/gh-orbit/internal/types"
)

// BeeepNotifier implements cross-platform notifications using the beeep library.
type BeeepNotifier struct {
	logger *slog.Logger
	status types.BridgeStatus
}

func NewBeeepNotifier(logger *slog.Logger) *BeeepNotifier {
	return &BeeepNotifier{
		logger: logger,
		status: types.StatusHealthy,
	}
}

func (n *BeeepNotifier) Notify(ctx context.Context, title, subtitle, body, url string, priority int) error {
	// beeep doesn't support subtitle separately, we join it with body
	msg := body
	if subtitle != "" {
		msg = subtitle + ": " + body
	}

	return beeep.Alert(title, msg, "")
}

func (n *BeeepNotifier) Shutdown(ctx context.Context) {
	n.logger.DebugContext(ctx, "beeep notifier shutdown complete")
}

func (n *BeeepNotifier) Status() types.BridgeStatus {
	return n.status
}
