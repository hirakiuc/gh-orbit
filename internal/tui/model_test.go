package tui

import (
	"fmt"
	"image/color"
	"log/slog"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/db"
	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *db.DB {
	// Use in-memory database for tests
	database, err := db.OpenInMemory(slog.Default())
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	return database
}

func TestModel_Update_SyncingState(t *testing.T) {
	styles := DefaultStyles(true)
	keys := DefaultKeyMap()
	l := list.New([]list.Item{}, newItemDelegate(styles, keys), 0, 0)

	m := &Model{
		syncing: true,
		list:    l,
		keys:    keys,
	}

	// notificationsLoadedMsg should NOT reset syncing
	msgLocal := notificationsLoadedMsg{}
	updatedModelLocal, _ := m.Update(msgLocal)
	if !updatedModelLocal.(*Model).syncing {
		t.Error("expected syncing to remain true after notificationsLoadedMsg")
	}

	// syncCompleteMsg SHOULD reset syncing
	msg := syncCompleteMsg{}
	updatedModel, _ := m.Update(msg)
	if updatedModel.(*Model).syncing {
		t.Error("expected syncing to be false after syncCompleteMsg")
	}

	// Test error reset
	m.syncing = true
	msgErr := errMsg{err: nil}
	updatedModel, _ = m.Update(msgErr)
	if updatedModel.(*Model).syncing {
		t.Error("expected syncing to be false after errMsg")
	}
}

func TestModel_Update_ThemeChange(t *testing.T) {
	styles := DefaultStyles(true)
	keys := DefaultKeyMap()
	l := list.New([]list.Item{}, newItemDelegate(styles, keys), 0, 0)

	m := &Model{
		styles: styles,
		list:   l,
		keys:   keys,
	}

	// Mock a light background color msg
	msg := tea.BackgroundColorMsg{Color: color.RGBA{R: 255, G: 255, B: 255, A: 255}}
	updatedModel, _ := m.Update(msg)
	_ = updatedModel.(*Model)

	// Since we can't easily check private style properties,
	// we've verified it compiles and runs.
}

func TestModel_Update_WindowSize(t *testing.T) {
	styles := DefaultStyles(true)
	keys := DefaultKeyMap()
	l := list.New([]list.Item{}, newItemDelegate(styles, keys), 0, 0)

	m := &Model{
		list: l,
		keys: keys,
	}

	// Mock window size msg
	width, height := 80, 24
	msg := tea.WindowSizeMsg{Width: width, Height: height}
	updatedModel, _ := m.Update(msg)
	newModel := updatedModel.(*Model)

	// The list height should be height-3 (Header, Tab bar, Footer)
	expectedHeight := height - 3
	if newModel.list.Height() != expectedHeight {
		t.Errorf("expected list height %d, got %d", expectedHeight, newModel.list.Height())
	}

	if newModel.list.Width() != width {
		t.Errorf("expected list width %d, got %d", width, newModel.list.Width())
	}
}

func TestModel_Update_StatusClearing(t *testing.T) {
	styles := DefaultStyles(true)
	keys := DefaultKeyMap()
	l := list.New([]list.Item{}, newItemDelegate(styles, keys), 0, 0)

	m := &Model{
		status: "some status",
		err:    fmt.Errorf("some error"),
		list:   l,
		keys:   keys,
	}

	// Test actionCompleteMsg
	msgAction := actionCompleteMsg{}
	updatedModel, _ := m.Update(msgAction)
	newModel := updatedModel.(*Model)
	if newModel.status != "" {
		t.Error("expected status to be cleared after actionCompleteMsg")
	}
	if newModel.err != nil {
		t.Error("expected err to be cleared after actionCompleteMsg")
	}

	// Test clearStatusMsg
	m.status = "temporary status"
	msgClear := clearStatusMsg{}
	updatedModel, _ = m.Update(msgClear)
	newModel = updatedModel.(*Model)
	if newModel.status != "" {
		t.Error("expected status to be cleared after clearStatusMsg")
	}
}

func TestModel_MarkRead(t *testing.T) {
	styles := DefaultStyles(true)
	keys := DefaultKeyMap()
	l := list.New([]list.Item{
		item{notification: db.NotificationWithState{
			Notification: db.Notification{GitHubID: "test-id"},
			OrbitState:   db.OrbitState{IsReadLocally: false},
		}},
	}, newItemDelegate(styles, keys), 0, 0)

	// Mock DB
	testDB := setupTestDB(t)
	defer func() { _ = testDB.Close() }()

	m := &Model{
		list:   l,
		db:     testDB,
		logger: slog.Default(),
	}

	// Initial state
	i := m.list.Items()[0].(item)
	if i.notification.IsReadLocally {
		t.Fatal("expected notification to be unread initially")
	}

	// Mark as read
	m.MarkRead(i)

	// Verify optimistic update
	updatedItem := m.list.Items()[0].(item)
	if !updatedItem.notification.IsReadLocally {
		t.Error("expected notification to be marked as read optimistically")
	}
}

func TestModel_TabFiltering(t *testing.T) {
	styles := DefaultStyles(true)
	keys := DefaultKeyMap()

	notifs := []db.NotificationWithState{
		{
			Notification: db.Notification{GitHubID: "unread-no-priority"},
			OrbitState:   db.OrbitState{IsReadLocally: false, Priority: 0},
		},
		{
			Notification: db.Notification{GitHubID: "read-with-priority"},
			OrbitState:   db.OrbitState{IsReadLocally: true, Priority: 1},
		},
		{
			Notification: db.Notification{GitHubID: "read-no-priority"},
			OrbitState:   db.OrbitState{IsReadLocally: true, Priority: 0},
		},
	}

	l := list.New([]list.Item{}, newItemDelegate(styles, keys), 0, 0)

	m := &Model{
		list:             l,
		allNotifications: notifs,
		activeTab:        TabInbox,
	}

	// 1. Test Inbox Tab (Default)
	m.applyFilters()
	if len(m.list.Items()) != 2 {
		t.Errorf("expected 2 items in Inbox, got %d", len(m.list.Items()))
	}

	// 2. Test Unread Tab
	m.activeTab = TabUnread
	m.applyFilters()
	if len(m.list.Items()) != 1 {
		t.Errorf("expected 1 item in Unread, got %d", len(m.list.Items()))
	}
	if m.list.Items()[0].(item).notification.GitHubID != "unread-no-priority" {
		t.Error("wrong item in Unread tab")
	}

	// 3. Test Triaged Tab
	m.activeTab = TabTriaged
	m.applyFilters()
	if len(m.list.Items()) != 1 {
		t.Errorf("expected 1 item in Triaged, got %d", len(m.list.Items()))
	}
	if m.list.Items()[0].(item).notification.GitHubID != "read-with-priority" {
		t.Error("wrong item in Triaged tab")
	}

	// 4. Test All Tab
	m.activeTab = TabAll
	m.applyFilters()
	if len(m.list.Items()) != 3 {
		t.Errorf("expected 3 items in All, got %d", len(m.list.Items()))
	}
}
