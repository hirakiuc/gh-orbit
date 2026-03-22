package tui

import (
	"context"
	"log/slog"
	"time"

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
	resourceFilter string // e.g. "PullRequest", "Issue"
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
	EnrichNotification(ctx context.Context, id, body, author, htmlURL, resourceState, resourceSubState string) error
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
	interpreter      *Interpreter

	// Background Sync State
	LastSyncAt        time.Time
	PollInterval      int
	RateLimit         models.RateLimitInfo
	heartbeatID       uint64
	clockID           uint64
	heartbeatInterval time.Duration
	clockInterval     time.Duration
	lastQuitPress     time.Time
	executor          types.CommandExecutor
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
	keys := DefaultKeyMap()
	delegate := newItemDelegate(styles, keys)

	l := list.New([]list.Item{}, delegate, 0, 0)
	l.Title = "GitHub Orbit"
	l.Styles.Title = styles.Title

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
		state:        StateList,
		PollInterval: cfg.Notifications.SyncInterval,
		// Default intervals
		heartbeatInterval: time.Duration(cfg.Notifications.SyncInterval) * time.Second,
		clockInterval:     1 * time.Minute,
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
		m.loadNotifications(),
		m.tickClock(),
		m.tickHeartbeat(),
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

func (m *Model) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	m.sync.Shutdown(ctx)
	m.enrich.Shutdown(ctx)
	m.traffic.Shutdown(ctx)
	m.alerter.Shutdown(ctx)
}

// loadNotifications loads the full list of notifications from local database.
func (m *Model) loadNotifications() tea.Cmd {
	return m.traffic.Submit(api.PrioritySync, func(ctx context.Context) tea.Msg {
		notifs, err := m.db.ListNotifications(ctx)
		if err != nil {
			return types.ErrMsg{Err: err}
		}
		// Initial load triggers initial enrichment
		return notificationsLoadedMsg{notifications: notifs, IsInitial: true}
	})
}

func (m *Model) syncNotificationsWithForce(force bool) tea.Cmd {
	return m.traffic.Submit(api.PrioritySync, func(ctx context.Context) tea.Msg {
		rl, err := m.sync.Sync(ctx, m.userID, force)
		if err != nil && err != types.ErrSyncIntervalNotReached {
			return types.ErrMsg{Err: err}
		}
		return syncCompleteMsg{rateLimit: rl}
	})
}

// Messages
type notificationsLoadedMsg struct {
	notifications []triage.NotificationWithState
	IsInitial     bool
}

type priorityUpdatedMsg struct {
	notifications []triage.NotificationWithState
	toast         string
}

type syncCompleteMsg struct {
	rateLimit models.RateLimitInfo
}

type detailLoadedMsg struct {
	GitHubID       string
	Body           string
	Author         string
	HTMLURL        string
	ResourceState  string
	ResourceSubState string
}

type (
	actionCompleteMsg struct{}
	clearStatusMsg    struct{}
	viewportEnrichMsg struct{}
)

type (
	pollTickMsg  struct{ ID uint64 }
	clockTickMsg struct{ ID uint64 }
)
