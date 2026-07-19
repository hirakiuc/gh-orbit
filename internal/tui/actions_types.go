package tui

import (
	"time"

	"github.com/hirakiuc/gh-orbit/internal/models"
	"github.com/hirakiuc/gh-orbit/internal/triage"
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
	Force    bool
	IsManual bool
}

func (a ActionSyncNotifications) Type() string { return "sync_notifications" }

type ActionCheckoutPR struct {
	NotificationID string
	Repository     string
	Number         string
}

func (a ActionCheckoutPR) Type() string { return "checkout_pr" }

type ActionStartReviewWorkspace struct {
	NotificationID    string
	Repository        types.ReviewWorkspaceRepository
	PullRequestNumber int
}

func (a ActionStartReviewWorkspace) Type() string { return "start_review_workspace" }

type ActionViewWeb struct {
	Notification triage.NotificationWithState
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

type ActionSetHandled struct {
	ID            string
	Handled       bool
	PreviousIndex int
}

func (a ActionSetHandled) Type() string { return "set_handled" }

type ActionApplyNotificationBatch struct {
	Request types.NotificationBatchRequest
}

func (a ActionApplyNotificationBatch) Type() string { return "apply_notification_batch" }

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
	SubjectType triage.SubjectType
	Force       bool
}

func (a ActionFetchDetail) Type() string { return "fetch_detail" }

type ActionShowToast struct {
	Message string
}

func (a ActionShowToast) Type() string { return "show_toast" }

type ActionSetSyncing struct {
	Enabled bool
}

func (a ActionSetSyncing) Type() string { return "set_syncing" }

type ActionSetFetching struct {
	Enabled bool
}

func (a ActionSetFetching) Type() string { return "set_fetching" }

type ActionEnrichItems struct {
	Notifications []triage.NotificationWithState
	Force         bool
}

func (a ActionEnrichItems) Type() string { return "enrich_items" }

type ActionLoadNotifications struct {
	IsInitial bool
	IsForced  bool
	IsManual  bool
}

func (a ActionLoadNotifications) Type() string { return "load_notifications" }

type ActionLoadBatchReconciliation struct {
	Generation uint64
}

func (a ActionLoadBatchReconciliation) Type() string { return "load_batch_reconciliation" }

type ActionUpdateRateLimit struct {
	Info models.RateLimitInfo
}

func (a ActionUpdateRateLimit) Type() string { return "update_rate_limit" }

type ActionScheduleTick struct {
	TickType TickType
	Interval time.Duration
}

func (a ActionScheduleTick) Type() string { return "schedule_tick" }

type ActionCheckFocusMode struct{}

func (a ActionCheckFocusMode) Type() string { return "check_focus_mode" }
