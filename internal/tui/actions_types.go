package tui

import (
	"time"

	"github.com/hirakiuc/gh-orbit/internal/types"
)

// Action defines the interface for decoupled TUI side-effects.
type Action interface {
	// Type returns a unique identifier for the action.
	Type() string
}

// TickType identifies the kind of scheduled event.
type TickType string

const (
	TickHeartbeat TickType = "heartbeat"
	TickClock     TickType = "clock"
	TickToast     TickType = "toast"
	TickEnrich    TickType = "enrich"
)

// Concrete Action implementations

type ActionQuit struct{}
func (a ActionQuit) Type() string { return "quit" }

type ActionSyncNotifications struct {
	Force bool
}
func (a ActionSyncNotifications) Type() string { return "sync_notifications" }

type ActionCheckoutPR struct {
	Repository string
	Number     string
}
func (a ActionCheckoutPR) Type() string { return "checkout_pr" }

type ActionViewWeb struct {
	Notification types.NotificationWithState
}
func (a ActionViewWeb) Type() string { return "view_web" }

type ActionOpenBrowser struct {
	URL string
}
func (a ActionOpenBrowser) Type() string { return "open_browser" }

type ActionMarkRead struct {
	ID   string
	Read bool
}
func (a ActionMarkRead) Type() string { return "mark_read" }

type ActionArchive struct {
	ID string
}
func (a ActionArchive) Type() string { return "archive" }

type ActionMute struct {
	ID string
}
func (a ActionMute) Type() string { return "mute" }

type ActionSetPriority struct {
	ID       string
	Priority int
}
func (a ActionSetPriority) Type() string { return "set_priority" }

type ActionFetchDetail struct {
	ID          string
	URL         string
	SubjectType string
}
func (a ActionFetchDetail) Type() string { return "fetch_detail" }

type ActionShowToast struct {
	Message string
}
func (a ActionShowToast) Type() string { return "show_toast" }

type ActionEnrichItems struct {
	Notifications []types.NotificationWithState
}
func (a ActionEnrichItems) Type() string { return "enrich_items" }

type ActionLoadNotifications struct{}
func (a ActionLoadNotifications) Type() string { return "load_notifications" }

type ActionUpdateRateLimit struct {
	Info types.RateLimitInfo
}
func (a ActionUpdateRateLimit) Type() string { return "update_rate_limit" }

type ActionScheduleTick struct {
	TickType TickType
	Interval time.Duration
}
func (a ActionScheduleTick) Type() string { return "schedule_tick" }
