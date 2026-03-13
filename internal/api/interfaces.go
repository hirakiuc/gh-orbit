package api

import (
	"context"

	"github.com/hirakiuc/gh-orbit/internal/github"
	"github.com/hirakiuc/gh-orbit/internal/types"
)

// Alerter defines the interface for the high-level alerting service.
type Alerter interface {
	Notify(ctx context.Context, n github.Notification) error
	SyncStart(ctx context.Context)
	Shutdown(ctx context.Context)
	ActiveTierInfo() (string, types.BridgeStatus)
	TestNotify(ctx context.Context, title, subtitle, body string) error
	BridgeStatus() types.BridgeStatus
}
