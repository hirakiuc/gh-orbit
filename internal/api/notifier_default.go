//go:build !darwin

package api

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gen2brain/beeep"
)

type defaultNotifier struct {
	logger *slog.Logger
}

// NewPlatformNotifier returns the default cross-platform notifier (beeep).
func NewPlatformNotifier(ctx context.Context, logger *slog.Logger) Notifier {
	return &defaultNotifier{logger: logger}
}

func (d *defaultNotifier) Notify(title, subtitle, body, url string, priority int) error {
	fullTitle := title
	if subtitle != "" {
		fullTitle = fmt.Sprintf("[%s] %s", subtitle, title)
	}

	return beeep.Notify(fullTitle, body, "")
}
