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

// Model represents the application state.
type Model struct {
	// Sub-Models
	listView   ListModel
	detailView DetailModel

	// Shared State & Services (Interfaces)
	db               api.Repository
	client           api.GitHubClient
	sync             api.Syncer
	enrich           api.Enricher
	traffic          api.TrafficController
	alerter          api.Alerter
	ui               UIController
	config           *config.Config
	logger           *slog.Logger
	userID           string
	version          string
	styles           Styles
	keys             KeyMap
	allNotifications []types.NotificationWithState
	err              error
	state            AppState
	isDark           bool
	markdownRenderer *glamour.TermRenderer
	width            int
	height           int
	headerHeight     int
	footerHeight     int
	bridgeStatus     api.BridgeStatus
	interpreter      *Interpreter

	// Background Sync State
	LastSyncAt        time.Time
	PollInterval      int
	RateLimit         types.RateLimitInfo
	heartbeatID       uint64
	clockID           uint64
	heartbeatInterval time.Duration
	clockInterval     time.Duration
	lastQuitPress     time.Time
	executor          api.CommandExecutor
}

// Option defines a functional option for Model configuration.
type Option func(*Model)

// WithExecutor sets the command executor.
func WithExecutor(executor api.CommandExecutor) Option {
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
	database api.Repository,
	client api.GitHubClient,
	syncer api.Syncer,
	enricher api.Enricher,
	traffic api.TrafficController,
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
		ui:           NewUIController(styles),
		config:       cfg,
		logger:       logger,
		userID:       userID,
		styles:       styles,
		keys:         keys,
		isDark:       true,
		state:             StateList,
		PollInterval:      cfg.Notifications.SyncInterval,
		LastSyncAt:        time.Now(),
		heartbeatInterval: time.Second,
		clockInterval:     time.Minute,
	}

	m.interpreter = NewInterpreter(m)

	for _, opt := range opts {
		opt(m)
	}

	return m
}

// Init initializes the application by loading data and starting background tasks.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		m.loadNotifications(),
		m.tickHeartbeat(),
		m.tickClock(),
	)
}

func (m *Model) loadNotifications() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		notifications, err := m.db.ListNotifications(ctx)
		if err != nil {
			return types.ErrMsg{Err: err}
		}
		return notificationsLoadedMsg{notifications: notifications, IsInitial: true}
	}
}

func (m *Model) syncNotificationsWithForce(force bool) tea.Cmd {
	return m.traffic.Submit(api.PrioritySync, func(ctx context.Context) tea.Msg {
		remaining, err := m.sync.Sync(ctx, m.userID, force)
		if err != nil {
			return types.ErrMsg{Err: err}
		}
		return syncCompleteMsg{rateLimit: remaining}
	})
}

func (m *Model) tickHeartbeat() tea.Cmd {
	m.heartbeatID++
	id := m.heartbeatID
	return tea.Tick(m.heartbeatInterval, func(t time.Time) tea.Msg {
		return pollTickMsg{ID: id}
	})
}

func (m *Model) tickClock() tea.Cmd {
	m.clockID++
	id := m.clockID
	return tea.Tick(m.clockInterval, func(t time.Time) tea.Msg {
		return clockTickMsg{ID: id}
	})
}

// Shutdown ensures all background services are stopped gracefully.
func (m *Model) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	m.sync.Shutdown(ctx)
	m.traffic.Shutdown(ctx)
	m.alerter.Shutdown(ctx)
}

// Internal messages
type notificationsLoadedMsg struct {
	notifications []types.NotificationWithState
	IsInitial     bool
}

type priorityUpdatedMsg struct {
	notifications []types.NotificationWithState
	toast         string
}

type syncCompleteMsg struct {
	rateLimit types.RateLimitInfo
}

type detailLoadedMsg struct {
	GitHubID      string
	Body          string
	Author        string
	HTMLURL       string
	ResourceState string
}

type actionCompleteMsg struct{}

type clearStatusMsg struct{}

type pollTickMsg struct {
	ID uint64
}

type clockTickMsg struct {
	ID uint64
}

type viewportEnrichMsg struct{}
