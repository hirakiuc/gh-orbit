package tui

import (
	"time"

	"github.com/hirakiuc/gh-orbit/internal/types"
)

// ActionType defines the category of an abstract TUI action.
type ActionType string

const (
	ActionTypeQuit              ActionType = "QUIT"
	ActionTypeShowToast         ActionType = "SHOW_TOAST"
	ActionTypeSyncNotifications ActionType = "SYNC_NOTIFICATIONS"
	ActionTypeMarkRead          ActionType = "MARK_READ"
	ActionTypeSetPriority       ActionType = "SET_PRIORITY"
	ActionTypeViewWeb           ActionType = "VIEW_WEB"
	ActionTypeCheckoutPR        ActionType = "CHECKOUT_PR"
	ActionTypeEnrichItems       ActionType = "ENRICH_ITEMS"
	ActionTypeScheduleTick      ActionType = "SCHEDULE_TICK"
	ActionTypeLoadNotifications ActionType = "LOAD_NOTIFICATIONS"
	ActionTypeUpdateRateLimit   ActionType = "UPDATE_RATE_LIMIT"
)

// Action represents an abstract side-effect intent from the TUI logic.
type Action interface {
	Type() ActionType
}

// ActionQuit signals the application to exit.
type ActionQuit struct{}

func (a ActionQuit) Type() ActionType { return ActionTypeQuit }

// ActionShowToast displays a transient message to the user.
type ActionShowToast struct {
	Message string
}

func (a ActionShowToast) Type() ActionType { return ActionTypeShowToast }

// ActionSyncNotifications triggers a synchronization cycle with GitHub.
type ActionSyncNotifications struct {
	Force bool
}

func (a ActionSyncNotifications) Type() ActionType { return ActionTypeSyncNotifications }

// ActionMarkRead updates the read status of a notification.
type ActionMarkRead struct {
	ID   string
	Read bool
}

func (a ActionMarkRead) Type() ActionType { return ActionTypeMarkRead }

// ActionSetPriority updates the priority level of a notification.
type ActionSetPriority struct {
	ID       string
	Priority int
}

func (a ActionSetPriority) Type() ActionType { return ActionTypeSetPriority }

// ActionViewWeb opens a notification's target in the system browser.
type ActionViewWeb struct {
	Notification types.NotificationWithState
}

func (a ActionViewWeb) Type() ActionType { return ActionTypeViewWeb }

// ActionCheckoutPR performs a local git checkout of a Pull Request.
type ActionCheckoutPR struct {
	Repository string
	Number     string
}

func (a ActionCheckoutPR) Type() ActionType { return ActionTypeCheckoutPR }

// ActionEnrichItems triggers metadata enrichment for a set of notifications.
type ActionEnrichItems struct {
	Notifications []types.NotificationWithState
}

func (a ActionEnrichItems) Type() ActionType { return ActionTypeEnrichItems }

// TickType distinguishes between different background timers.
type TickType string

const (
	TickHeartbeat TickType = "HEARTBEAT"
	TickClock     TickType = "CLOCK"
	TickToast     TickType = "TOAST"
	TickEnrich    TickType = "ENRICH"
)

// ActionScheduleTick schedules a future message delivery (timer).
type ActionScheduleTick struct {
	TickType TickType
	Interval time.Duration
}

func (a ActionScheduleTick) Type() ActionType { return ActionTypeScheduleTick }

// ActionLoadNotifications triggers a local data reload without remote sync.
type ActionLoadNotifications struct{}

func (a ActionLoadNotifications) Type() ActionType { return ActionTypeLoadNotifications }

// ActionUpdateRateLimit updates the local rate limit status.
type ActionUpdateRateLimit struct {
	Remaining int
}

func (a ActionUpdateRateLimit) Type() ActionType { return ActionTypeUpdateRateLimit }
