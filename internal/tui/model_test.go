package tui

import (
	"image/color"
	"log/slog"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/db"
	"github.com/charmbracelet/x/exp/golden"
	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *db.DB {
	database, err := db.OpenInMemory(slog.Default())
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	return database
}

func newTestModel(t *testing.T) *Model {
	styles := DefaultStyles(true)
	keys := DefaultKeyMap()
	delegate := newItemDelegate(styles, keys)
	l := list.New([]list.Item{}, delegate, 80, 24)
	
	database := setupTestDB(t)
	
	m := &Model{
		list:   l,
		db:     database,
		keys:   keys,
		styles: styles,
		ui:     NewUIController(styles),
		logger: slog.Default(),
		state:  StateList,
	}
	m.ui.SetSize(80, 24)
	return m
}

func TestModel_Update_SyncingState(t *testing.T) {
	m := newTestModel(t)
	m.ui.SetSyncing(true)

	// syncCompleteMsg SHOULD reset syncing
	msg := syncCompleteMsg{}
	updatedModel, _ := m.Update(msg)
	if updatedModel.(*Model).ui.syncing {
		t.Error("expected syncing to be false after syncCompleteMsg")
	}

	// Test error reset
	m.ui.SetSyncing(true)
	msgErr := errMsg{err: nil}
	updatedModel, _ = m.Update(msgErr)
	if updatedModel.(*Model).ui.syncing {
		t.Error("expected syncing to be false after errMsg")
	}
}

func TestModel_Update_ThemeChange(t *testing.T) {
	m := newTestModel(t)

	// Mock a light background color msg
	msg := tea.BackgroundColorMsg{Color: color.RGBA{R: 255, G: 255, B: 255, A: 255}}
	updatedModel, _ := m.Update(msg)
	_ = updatedModel.(*Model)
}

func TestModel_Update_StatusClearing(t *testing.T) {
	m := newTestModel(t)
	m.ui.SetToast("temporary status")

	msgClear := clearStatusMsg{}
	updatedModel, _ := m.Update(msgClear)
	newModel := updatedModel.(*Model)
	if newModel.ui.toastMessage != "" {
		t.Error("expected status to be cleared after clearStatusMsg")
	}
}

func TestModel_MarkRead(t *testing.T) {
	m := newTestModel(t)
	m.allNotifications = []db.NotificationWithState{{
		Notification: db.Notification{GitHubID: "test-id"},
		OrbitState:   db.OrbitState{IsReadLocally: false},
	}}
	m.activeTab = TabAll
	m.applyFilters()

	// Initial state
	i := m.list.Items()[0].(item)
	m.MarkRead(i)

	if !m.allNotifications[0].IsReadLocally {
		t.Error("expected allNotifications[0] to be marked as read")
	}
}

func TestModel_ResourceFiltering(t *testing.T) {
	m := newTestModel(t)
	m.allNotifications = []db.NotificationWithState{
		{Notification: db.Notification{GitHubID: "1", SubjectType: "PullRequest"}},
		{Notification: db.Notification{GitHubID: "2", SubjectType: "Issue"}},
	}
	m.activeTab = TabAll

	// 1. Filter PRs
	m.toggleResourceFilter("PullRequest", "PRs")
	if len(m.list.Items()) != 1 {
		t.Errorf("expected 1 item (PR), got %d", len(m.list.Items()))
	}

	// 2. Clear
	m.toggleResourceFilter("PullRequest", "PRs")
	if len(m.list.Items()) != 2 {
		t.Errorf("expected 2 items after clear, got %d", len(m.list.Items()))
	}
}

func TestView_Golden(t *testing.T) {
	m := newTestModel(t)
	m.allNotifications = []db.NotificationWithState{
		{
			Notification: db.Notification{
				GitHubID: "1", 
				SubjectTitle: "Feature Refactoring", 
				SubjectType: "PullRequest",
				RepositoryFullName: "hirakiuc/gh-orbit",
			},
			OrbitState: db.OrbitState{Priority: 3},
		},
	}
	m.applyFilters()

	// 1. Snapshot List View
	golden.RequireEqual(t, m.View().Content)

	// 2. Snapshot Detail View
	m.state = StateDetail
	m.viewport.SetContent("Detail content")
	golden.RequireEqual(t, m.View().Content)
}
