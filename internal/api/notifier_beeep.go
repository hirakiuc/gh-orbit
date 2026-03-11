package api

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gen2brain/beeep"
)

type beeepNotifier struct {
	logger *slog.Logger
}

// NewBeeepNotifier returns a cross-platform notifier using the beeep library.
func NewBeeepNotifier(logger *slog.Logger) Notifier {
	return &beeepNotifier{
		logger: logger,
	}
}

func (b *beeepNotifier) Notify(ctx context.Context, title, subtitle, body, url string, priority int) error {
	b.logger.DebugContext(ctx, "delivering notification via beeep fallback", "title", title)
	
	// Beeep doesn't natively support subtitles on all platforms, so we combine them.
	fullTitle := title
	if subtitle != "" {
		fullTitle = fmt.Sprintf("%s: %s", title, subtitle)
	}

	// Beeep.Notify is already asynchronous on most platforms, but we follow our fire-and-forget pattern.
	go func() {
		// Use request context (WithoutCancel) to satisfy G118 while ensuring delivery.
		backgroundCtx := context.WithoutCancel(ctx)
		if err := beeep.Notify(fullTitle, body, ""); err != nil {
			b.logger.WarnContext(backgroundCtx, "beeep notification failed", "error", err)
		}
	}()

	return nil
}

func (b *beeepNotifier) Shutdown(ctx context.Context) {}

func (b *beeepNotifier) Status() BridgeStatus {
	return StatusHealthy
}
