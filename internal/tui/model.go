package tui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/glamour"
	"github.com/hirakiuc/gh-orbit/internal/api"
	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/models"
	"github.com/hirakiuc/gh-orbit/internal/triage"
	"github.com/hirakiuc/gh-orbit/internal/types"
)

type AppState int

const (
	TabInbox = iota
	TabTriaged
	TabAll
	tabCount
)

const (
	StateList AppState = iota
	StateDetail
)

// ListModel encapsulates list-specific state.
type ListModel struct {
	list           list.Model
	delegate       itemDelegate
	activeTab      int
	resourceFilter triage.SubjectType // e.g. "PullRequest", "Issue"
}

// DetailModel encapsulates viewport-specific state.
type DetailModel struct {
	viewport          viewport.Model
	activeDetail      string // Rendered markdown
	lastRenderedID    string
	lastRenderedWidth int
}

// Model represents the application state.
type Model struct {
	// Sub-Models
	listView   ListModel
	detailView DetailModel

	// Shared State & Services (Interfaces)
	backend             types.TUIBackend
	traffic             types.TrafficController
	alerter             api.Alerter
	ui                  UIController
	config              *config.Config
	logger              *slog.Logger
	userID              string
	version             string
	styles              Styles
	keys                KeyMap
	help                *help.Model
	showHelp            bool
	allNotifications    []triage.NotificationWithState
	selectionMode       bool
	selectedIDs         map[string]struct{}
	batchPending        bool
	batchUncertain      bool
	batchRefreshPending bool
	pendingBatchRequest types.NotificationBatchRequest
	err                 error
	state               AppState
	isDark              bool
	markdownRenderer    *glamour.TermRenderer
	width               int
	height              int
	headerHeight        int
	footerHeight        int
	bridgeStatus        types.BridgeStatus
	focusMode           string
	interpreter         *Interpreter

	// Connection Mode
	ConnectionMode string // "Standalone" or "Connected"

	// Background Sync State
	LastSyncAt        time.Time
	PollInterval      int
	RateLimit         models.RateLimitInfo
	QuotaResetStatus  string // Cached humanized reset duration
	heartbeatID       uint64
	clockID           uint64
	enrichID          uint64
	heartbeatInterval time.Duration
	clockInterval     time.Duration

	// Enrichment Management
	inflightEnrichments map[string]time.Time
	taskCancelMu        sync.Mutex
	taskCancelSeq       uint64
	taskCancels         map[string]scopedTaskCancel
	taskRoot            context.Context
	ownsSubsystems      bool
	manualSyncPending   bool
	manualSyncSnapshot  string

	lastQuitPress time.Time
	executor      types.CommandExecutor

	syncStarted bool
}

type scopedTaskCancel struct {
	id     uint64
	cancel context.CancelFunc
}

// Option defines a functional option for Model configuration.
type Option func(*Model)

// WithExecutor sets the command executor.
func WithExecutor(executor types.CommandExecutor) Option {
	return func(m *Model) {
		m.executor = executor
	}
}

// WithTheme sets the initial theme.
func WithTheme(isDark bool) Option {
	return func(m *Model) {
		m.isDark = isDark
	}
}

// WithVersion sets the application version.
func WithVersion(v string) Option {
	return func(m *Model) {
		m.version = v
	}
}

// WithConnectionMode sets the engine connection mode (Standalone/Connected).
func WithConnectionMode(mode string) Option {
	return func(m *Model) {
		m.ConnectionMode = mode
	}
}

// WithOwnedSubsystemShutdown makes Model.Shutdown responsible only for
// host-local injected cleanup surfaces. Shared standalone runtime services must
// still be owned by the outer runtime, not by the TUI model.
func WithOwnedSubsystemShutdown() Option {
	return func(m *Model) {
		m.ownsSubsystems = true
	}
}

// ModelParams contains dependencies for the TUI Model.
type ModelParams struct {
	UserID   string
	Config   *config.Config
	Logger   *slog.Logger
	TaskRoot context.Context
	Backend  types.TUIBackend
	Traffic  types.TrafficController
	Alerter  api.Alerter
	Options  []Option
}

// NewModel initializes a new application model with dependency injection.
func NewModel(p ModelParams) (*Model, error) {
	if p.UserID == "" {
		return nil, fmt.Errorf("user ID is required for TUI")
	}
	if p.Config == nil {
		return nil, fmt.Errorf("config is required for TUI")
	}
	if p.Logger == nil {
		return nil, fmt.Errorf("logger is required for TUI")
	}
	if p.TaskRoot == nil {
		return nil, fmt.Errorf("task root context is required for TUI")
	}
	if p.Backend == nil {
		return nil, fmt.Errorf("backend is required for TUI")
	}
	if p.Alerter == nil {
		return nil, fmt.Errorf("alerter is required for TUI")
	}

	styles := DefaultStyles(true)
	keys := NewKeyMap(p.Config)
	selectedIDs := make(map[string]struct{})
	delegate := newItemDelegate(styles, keys, selectedIDs)

	l := list.New([]list.Item{}, delegate, 0, 0)
	l.Title = "GitHub Orbit"
	l.Styles.Title = styles.Title
	l.SetShowHelp(false)

	h := help.New()
	h.Styles.ShortKey = styles.Help
	h.Styles.ShortDesc = styles.Help
	h.Styles.FullKey = styles.Help
	h.Styles.FullDesc = styles.Help

	vp := viewport.New()
	vp.MouseWheelEnabled = true
	vp.SoftWrap = false // We handle wrapping manually via lipgloss in refreshDetailView

	m := &Model{
		listView: ListModel{
			list:     l,
			delegate: delegate,
		},
		detailView: DetailModel{
			viewport: vp,
		},
		backend:      p.Backend,
		traffic:      p.Traffic,
		alerter:      p.Alerter,
		config:       p.Config,
		logger:       p.Logger,
		userID:       p.UserID,
		styles:       styles,
		keys:         keys,
		help:         &h,
		selectedIDs:  selectedIDs,
		state:        StateList,
		PollInterval: p.Config.Notifications.SyncInterval,
		// Default intervals
		heartbeatInterval:   time.Duration(p.Config.Notifications.SyncInterval) * time.Second,
		clockInterval:       1 * time.Minute,
		inflightEnrichments: make(map[string]time.Time),
		taskCancels:         make(map[string]scopedTaskCancel),
		taskRoot:            p.TaskRoot,
		executor:            api.NewOSCommandExecutor(), // Default executor
	}

	// Apply options
	for _, opt := range p.Options {
		opt(m)
	}

	m.interpreter = NewInterpreter(m)
	m.ui = NewUIController(m.styles)

	return m, nil
}

// Init sets up initial application state and background workers.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		m.loadNotifications(true, false, false),
		m.tickClock(),
		m.checkFocusMode(),
	)
}

func (m *Model) tickHeartbeat() tea.Cmd {
	m.heartbeatID++
	id := m.heartbeatID
	return tea.Tick(m.heartbeatInterval, func(_ time.Time) tea.Msg {
		return pollTickMsg{ID: id}
	})
}

func (m *Model) tickClock() tea.Cmd {
	m.clockID++
	id := m.clockID
	return tea.Tick(m.clockInterval, func(_ time.Time) tea.Msg {
		return clockTickMsg{ID: id}
	})
}

func (m *Model) tickEnrich() tea.Cmd {
	m.enrichID++
	id := m.enrichID
	debounce := time.Duration(m.config.Enrichment.DebounceMS) * time.Millisecond
	return tea.Tick(debounce, func(_ time.Time) tea.Msg {
		return viewportEnrichMsg{ID: id}
	})
}

func (m *Model) Shutdown() {
	m.cancelScopedTasks()

	if !m.ownsSubsystems {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if m.backend != nil {
		m.backend.Shutdown(ctx)
	}
	if m.traffic != nil {
		m.traffic.Shutdown(ctx)
	}
	if m.alerter != nil {
		m.alerter.Shutdown(ctx)
	}
}

func (m *Model) submitTask(scope string, timeout time.Duration, priority int, fn types.TaskFunc) tea.Cmd {
	return func() tea.Msg {
		ctx, release := m.newTaskContext(scope, timeout)
		defer release()

		if m.traffic == nil {
			// In MCP mode or if traffic controller is missing, execute immediately
			if ctx.Err() != nil {
				return nil
			}
			return fn(ctx)
		}

		resChan, err := m.traffic.Submit(ctx, priority, fn)
		if err != nil {
			return types.ErrMsg{Err: err}
		}
		return <-resChan
	}
}

func (m *Model) submitBackendTask(scope string, timeout time.Duration, fn types.TaskFunc) tea.Cmd {
	return func() tea.Msg {
		ctx, release := m.newTaskContext(scope, timeout)
		defer release()
		return fn(ctx)
	}
}

func (m *Model) newTaskContext(scope string, timeout time.Duration) (context.Context, func()) {
	base := m.taskRoot
	var (
		ctx    context.Context
		cancel context.CancelFunc
	)
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(base, timeout)
	} else {
		ctx, cancel = context.WithCancel(base)
	}

	if scope == "" {
		return ctx, cancel
	}

	m.taskCancelMu.Lock()
	m.taskCancelSeq++
	id := m.taskCancelSeq
	if prev, ok := m.taskCancels[scope]; ok {
		prev.cancel()
	}
	m.taskCancels[scope] = scopedTaskCancel{id: id, cancel: cancel}
	m.taskCancelMu.Unlock()

	return ctx, func() {
		cancel()
		m.taskCancelMu.Lock()
		if current, ok := m.taskCancels[scope]; ok && current.id == id {
			delete(m.taskCancels, scope)
		}
		m.taskCancelMu.Unlock()
	}
}

func (m *Model) cancelScopedTasks() {
	m.taskCancelMu.Lock()
	cancels := make([]context.CancelFunc, 0, len(m.taskCancels))
	for _, scoped := range m.taskCancels {
		cancels = append(cancels, scoped.cancel)
	}
	clear(m.taskCancels)
	m.taskCancelMu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
}

// loadNotifications loads the full list of notifications from local database.
func (m *Model) loadNotifications(isInitial bool, isForced bool, isManual bool) tea.Cmd {
	return m.submitTask("notifications:load", 0, api.PrioritySync, func(ctx context.Context) any {
		notifs, err := m.backend.ListNotifications(ctx)
		if err != nil {
			return types.ErrMsg{Err: err}
		}
		// Initial load triggers initial enrichment
		return notificationsLoadedMsg{notifications: notifs, IsInitial: isInitial, IsForced: isForced, IsManual: isManual}
	})
}

func (m *Model) syncNotificationsWithForce(force bool, isManual bool) tea.Cmd {
	return m.submitTask("notifications:sync", types.ConnectedSyncTimeout, api.PrioritySync, func(ctx context.Context) any {
		rl, err := m.backend.Sync(ctx, force)
		if err != nil && !errors.Is(err, types.ErrSyncIntervalNotReached) {
			return types.ErrMsg{Err: err}
		}
		return syncCompleteMsg{rateLimit: rl, IsForced: force, IsManual: isManual}
	})
}

func (m *Model) updateQuotaResetStatus() {
	if m.RateLimit.Reset.IsZero() {
		m.QuotaResetStatus = ""
		return
	}

	m.QuotaResetStatus = time.Until(m.RateLimit.Reset).Round(time.Minute).String()
}

func (m *Model) checkFocusMode() tea.Cmd {
	return func() tea.Msg {
		return focusModeMsg(api.CheckFocusMode(m.executor))
	}
}

// Messages
type focusModeMsg string

type notificationsLoadedMsg struct {
	notifications []triage.NotificationWithState
	IsInitial     bool
	IsForced      bool
	IsManual      bool
}

type mutationAppliedMsg struct {
	notifications []triage.NotificationWithState
	toast         string
	err           error
	targetID      string
	previousIndex int
	reconcileItem bool
}

type batchMutationAppliedMsg struct {
	result types.NotificationBatchResult
	before []triage.NotificationWithState
}

type reviewWorkspaceStartedMsg struct {
	toast string
}

type syncCompleteMsg struct {
	rateLimit models.RateLimitInfo
	IsForced  bool
	IsManual  bool
}
type detailLoadedMsg struct {
	GitHubID         string
	SubjectNodeID    string
	Body             string
	Author           string
	HTMLURL          string
	ResourceState    string
	ResourceSubState string
}

type enrichmentBatchCompleteMsg struct {
	Results map[string]models.EnrichmentResult
}

type (
	actionCompleteMsg struct{}
	clearStatusMsg    struct{}
	viewportEnrichMsg struct{ ID uint64 }
)

type (
	pollTickMsg  struct{ ID uint64 }
	clockTickMsg struct{ ID uint64 }
)

func notificationStateSignature(notifications []triage.NotificationWithState) string {
	return fmt.Sprintf("%#v", notifications)
}
