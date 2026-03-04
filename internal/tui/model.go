package tui

import (
	"context"
	"log/slog"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/glamour"
	"github.com/hirakiuc/gh-orbit/internal/api"
	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/db"
)

const (
	TabInbox = iota
	TabUnread
	TabTriaged
	TabAll
)

type AppState int

const (
	StateList AppState = iota
	StateDetail
)

// ListModel encapsulates the state for the notification list view.
type ListModel struct {
	list           list.Model
	delegate       itemDelegate
	activeTab      int
	resourceFilter string
}

// DetailModel encapsulates the state for the notification detail view.
type DetailModel struct {
	viewport     viewport.Model
	activeDetail string
}

type Model struct {
	// Sub-Models
	listView   ListModel
	detailView DetailModel

	// Shared State & Services
	db               *db.DB
	client           *api.Client
	sync             *api.SyncEngine
	enrich           *api.EnrichmentEngine
	traffic          *api.APITrafficController
	ui               UIController
	config           *config.Config
	logger           *slog.Logger
	userID           string
	styles           Styles
	keys             KeyMap
	allNotifications []db.NotificationWithState
	err              error
	state            AppState
	isDark           bool
	markdownRenderer *glamour.TermRenderer
	width            int
	height           int
}

func NewModel(database *db.DB, client *api.Client, userID string, cfg *config.Config, logger *slog.Logger) Model {
	styles := DefaultStyles(true) // Default to dark theme
	keys := DefaultKeyMap()
	delegate := newItemDelegate(styles, keys)

	l := list.New([]list.Item{}, delegate, 0, 0)
	l.Title = "GitHub Orbit"
	l.Styles.Title = styles.Title

	vp := viewport.New()

	alerts := api.NewAlertService(cfg, database, logger)
	fetcher := api.NewNotificationFetcher(client, logger)

	return Model{
		listView: ListModel{
			list:     l,
			delegate: delegate,
		},
		detailView: DetailModel{
			viewport: vp,
		},
		db:       database,
		client:   client,
		sync:     api.NewSyncEngine(fetcher, database, alerts, logger),
		enrich:   api.NewEnrichmentEngine(client, database, logger),
		traffic:  api.NewAPITrafficController(logger),
		ui:       NewUIController(styles),
		config:   cfg,
		logger:   logger,
		userID:   userID,
		styles:   styles,
		keys:     keys,
		isDark:   true,
		state:    StateList,
	}
}

func (m *Model) Init() tea.Cmd {
	return tea.Sequence(
		m.loadNotifications(),
		tea.Batch(
			tea.RequestBackgroundColor,
			m.ui.SetSyncing(true),
			m.syncNotifications(),
		),
	)
}

// Msg types
type (
	notificationsLoadedMsg []db.NotificationWithState
	syncCompleteMsg        []db.NotificationWithState
	actionCompleteMsg      struct{}
	clearStatusMsg         struct{}
	viewportEnrichMsg      struct{}
	detailLoadedMsg        struct {
		GitHubID      string
		Body          string
		Author        string
		HTMLURL       string
		ResourceState string
	}
	errMsg struct{ err error }
)

func (m *Model) loadNotifications() tea.Cmd {
	return func() tea.Msg {
		notifs, err := m.db.ListNotifications()
		if err != nil {
			return errMsg{err}
		}
		return notificationsLoadedMsg(notifs)
	}
}

func (m *Model) syncNotifications() tea.Cmd {
	return m.traffic.Submit(api.PriorityUser, func(ctx context.Context) tea.Msg {
		remaining, err := m.sync.Sync(m.userID)
		if err != nil {
			return errMsg{err}
		}
		
		// Update traffic controller with latest quota info
		m.traffic.UpdateRateLimit(remaining)

		// Reload after sync
		notifs, err := m.db.ListNotifications()
		if err != nil {
			return errMsg{err}
		}
		return syncCompleteMsg(notifs)
	})
}
