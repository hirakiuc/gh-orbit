package tui

import (
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/api"
	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/db"
)

type Model struct {
	list   list.Model
	db     *db.DB
	client *api.Client
	sync   *api.SyncEngine
	config *config.Config
	userID string
	styles Styles
	err    error
	status string
}

func NewModel(database *db.DB, client *api.Client, userID string, cfg *config.Config) Model {
	styles := DefaultStyles()
	delegate := newItemDelegate(styles)

	l := list.New([]list.Item{}, delegate, 0, 0)
	l.Title = "GitHub Orbit"
	l.Styles.Title = styles.Title

	alerts := api.NewAlertService(cfg)

	return Model{
		list:   l,
		db:     database,
		client: client,
		sync:   api.NewSyncEngine(client, database, alerts),
		config: cfg,
		userID: userID,
		styles: styles,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Sequence(
		m.loadNotifications(),
		m.syncNotifications(),
	)
}

// Msg types
type (
	notificationsLoadedMsg []db.NotificationWithState
	errMsg                 struct{ err error }
)

func (m Model) loadNotifications() tea.Cmd {
	return func() tea.Msg {
		notifs, err := m.db.ListNotifications()
		if err != nil {
			return errMsg{err}
		}
		return notificationsLoadedMsg(notifs)
	}
}

func (m Model) syncNotifications() tea.Cmd {
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
		return notificationsLoadedMsg(notifs)
	}
}
