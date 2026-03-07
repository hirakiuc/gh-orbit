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
	return &beeepNotifier{logger: logger}
}

func (b *beeepNotifier) Notify(ctx context.Context, title, subtitle, body, url string, priority int) error {
	fullTitle := title
	if subtitle != "" {
		fullTitle = fmt.Sprintf("[%s] %s", subtitle, title)
	}

	b.logger.DebugContext(ctx, "delivering notification via beeep fallback", "title", fullTitle)
	return beeep.Notify(fullTitle, body, "")
}

func (b *beeepNotifier) Shutdown(ctx context.Context) {}

func (b *beeepNotifier) Warmup() {}

func (b *beeepNotifier) Ready() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func (b *beeepNotifier) Status() BridgeStatus {
	return StatusHealthy
}
