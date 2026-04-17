package tui

import (
	"context"
	"log/slog"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/glamour"
	"github.com/hirakiuc/gh-orbit/internal/api"
	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/github"
	"github.com/hirakiuc/gh-orbit/internal/models"
	"github.com/hirakiuc/gh-orbit/internal/triage"
	"github.com/hirakiuc/gh-orbit/internal/types"
)

type AppState int

const (
	TabInbox = iota
	TabUnread
	TabTriaged
	TabAll
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

// notificationStore defines the notification persistence behavior the TUI needs.
type notificationStore interface {
	ListNotifications(ctx context.Context) ([]triage.NotificationWithState, error)
	MarkReadLocally(ctx context.Context, id string, isRead bool) error
	SetPriority(ctx context.Context, id string, priority int) error
	EnrichNotification(ctx context.Context, id, nodeID, body, author, htmlURL, resourceState, resourceSubState string) error
}

// Model represents the application state.
type Model struct {
	// Sub-Models
	listView   ListModel
	detailView DetailModel

	// Shared State & Services (Interfaces)
	db               notificationStore
	client           github.Client
	sync             types.Syncer
	enrich           types.Enricher
	traffic          types.TrafficController
	alerter          api.Alerter
	ui               UIController
	config           *config.Config
	logger           *slog.Logger
	userID           string
	version          string
	styles           Styles
	keys             KeyMap
	help             *help.Model
	showHelp         bool
	allNotifications []triage.NotificationWithState
	err              error
	state            AppState
	isDark           bool
	markdownRenderer *glamour.TermRenderer
	width            int
	height           int
	headerHeight     int
	footerHeight     int
	bridgeStatus     types.BridgeStatus
	focusMode        string
	interpreter      *Interpreter

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

	lastQuitPress time.Time
	executor      types.CommandExecutor

	syncStarted bool
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

// NewModel initializes a new application model with dependency injection.
func NewModel(
	userID string,
	cfg *config.Config,
	logger *slog.Logger,
	database notificationStore,
	client github.Client,
	syncer types.Syncer,
	enricher types.Enricher,
	traffic types.TrafficController,
	alerter api.Alerter,
	opts ...Option,
) *Model {
	styles := DefaultStyles(true)
	keys := NewKeyMap(cfg)
	delegate := newItemDelegate(styles, keys)

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
		db:           database,
		client:       client,
		sync:         syncer,
		enrich:       enricher,
		traffic:      traffic,
		alerter:      alerter,
		config:       cfg,
		logger:       logger,
		userID:       userID,
		styles:       styles,
		keys:         keys,
		help:         &h,
		state:        StateList,
		PollInterval: cfg.Notifications.SyncInterval,
		// Default intervals
		heartbeatInterval:   time.Duration(cfg.Notifications.SyncInterval) * time.Second,
		clockInterval:       1 * time.Minute,
		inflightEnrichments: make(map[string]time.Time),
	}

	// Apply options
	for _, opt := range opts {
		opt(m)
	}

	m.interpreter = NewInterpreter(m)
	m.ui = NewUIController(m.styles)

	return m
}

// Init sets up initial application state and background workers.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		m.loadNotifications(true, false),
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	m.sync.Shutdown(ctx)
	m.enrich.Shutdown(ctx)
	m.traffic.Shutdown(ctx)
	m.alerter.Shutdown(ctx)
}

func (m *Model) submitTask(priority int, fn types.TaskFunc) tea.Cmd {
	return func() tea.Msg {
		resChan, err := m.traffic.Submit(priority, fn)
		if err != nil {
			return types.ErrMsg{Err: err}
		}
		return <-resChan
	}
}

// loadNotifications loads the full list of notifications from local database.
func (m *Model) loadNotifications(isInitial bool, isForced bool) tea.Cmd {
	return m.submitTask(api.PrioritySync, func(ctx context.Context) any {
		notifs, err := m.db.ListNotifications(ctx)
		if err != nil {
			return types.ErrMsg{Err: err}
		}
		// Initial load triggers initial enrichment
		return notificationsLoadedMsg{notifications: notifs, IsInitial: isInitial, IsForced: isForced}
	})
}

func (m *Model) syncNotificationsWithForce(force bool) tea.Cmd {
	return m.submitTask(api.PrioritySync, func(ctx context.Context) any {
		rl, err := m.sync.Sync(ctx, m.userID, force)
		if err != nil && err != types.ErrSyncIntervalNotReached {
			return types.ErrMsg{Err: err}
		}
		return syncCompleteMsg{rateLimit: rl, IsForced: force}
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
}

type priorityUpdatedMsg struct {
	notifications []triage.NotificationWithState
	toast         string
}

type syncCompleteMsg struct {
	rateLimit models.RateLimitInfo
	IsForced  bool
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
