package tui

import (
	"log/slog"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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

type Model struct {
	list             list.Model
	db               *db.DB
	client           *api.Client
	sync             *api.SyncEngine
	config           *config.Config
	logger           *slog.Logger
	userID           string
	styles           Styles
	keys             KeyMap
	activeTab        int
	allNotifications []db.NotificationWithState
	err              error
	status           string
	spinner          spinner.Model
	syncing          bool
	viewport         viewport.Model
	showDetail       bool
	fetchingDetail   bool
	activeDetail     string
	isDark           bool
	markdownRenderer *glamour.TermRenderer
	toastMessage     string
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

	s := spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4"))),
	)

	vp := viewport.New()

	alerts := api.NewAlertService(cfg, logger)
	fetcher := api.NewNotificationFetcher(client, logger)

	return Model{
		list:     l,
		db:       database,
		client:   client,
		sync:     api.NewSyncEngine(fetcher, database, alerts, logger),
		config:   cfg,
		logger:   logger,
		userID:   userID,
		styles:   styles,
		keys:     keys,
		spinner:  s,
		viewport: vp,
		isDark:   true,
	}
}

func (m *Model) Init() tea.Cmd {
	m.syncing = true
	return tea.Sequence(
		m.loadNotifications(),
		tea.Batch(
			tea.RequestBackgroundColor,
			m.syncNotifications(),
			m.spinner.Tick,
		),
	)
}

// Msg types
type (
	notificationsLoadedMsg []db.NotificationWithState
	syncCompleteMsg        []db.NotificationWithState
	actionCompleteMsg      struct{}
	clearStatusMsg         struct{}
	detailLoadedMsg        struct {
		GitHubID string
		Body     string
		Author   string
		HTMLURL  string
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
	return func() tea.Msg {
		err := m.sync.Sync(m.userID)
		if err != nil {
			return errMsg{err}
		}
		// Reload after sync
		notifs, err := m.db.ListNotifications()
		if err != nil {
			return errMsg{err}
		}
		return syncCompleteMsg(notifs)
	}
}
