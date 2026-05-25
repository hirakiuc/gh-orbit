package main

import (
	"context"

	"github.com/hirakiuc/gh-orbit/internal/api"
	"github.com/hirakiuc/gh-orbit/internal/github"
	"github.com/hirakiuc/gh-orbit/internal/types"
)

// connectedModeAlerter keeps the connected-mode alert requirement explicit at
// the TUI wiring layer without re-expanding MCPAdapter into a pseudo-runtime
// facade.
type connectedModeAlerter struct{}

var _ api.Alerter = connectedModeAlerter{}

func (connectedModeAlerter) Notify(context.Context, github.Notification) error { return nil }
func (connectedModeAlerter) SyncStart(context.Context)                         {}
func (connectedModeAlerter) Shutdown(context.Context)                          {}
func (connectedModeAlerter) ActiveTierInfo() (string, types.BridgeStatus) {
	return "Connected", types.StatusHealthy
}
func (connectedModeAlerter) TestNotify(context.Context, string, string, string) error { return nil }
func (connectedModeAlerter) BridgeStatus() types.BridgeStatus                         { return types.StatusHealthy }
