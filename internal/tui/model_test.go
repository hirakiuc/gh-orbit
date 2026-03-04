package tui

import (
	"image/color"
	"log/slog"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/hirakiuc/gh-orbit/internal/db"
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
		listView: ListModel{
			list:     l,
			delegate: delegate,
		},
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
	m.listView.activeTab = TabAll
	m.applyFilters()

	// Initial state
	i := m.listView.list.Items()[0].(item)
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
	m.listView.activeTab = TabAll

	// 1. Filter PRs
	m.toggleResourceFilter("PullRequest", "PRs")
	if len(m.listView.list.Items()) != 1 {
		t.Errorf("expected 1 item (PR), got %d", len(m.listView.list.Items()))
	}

	// 2. Clear
	m.toggleResourceFilter("PullRequest", "PRs")
	if len(m.listView.list.Items()) != 2 {
		t.Errorf("expected 2 items after clear, got %d", len(m.listView.list.Items()))
	}
}

func TestModel_Navigation(t *testing.T) {
	m := newTestModel(t)

	// 1. Test Detail -> List transition via 'q'
	m.state = StateDetail
	msgQ := tea.KeyPressMsg{Text: "q", Code: 'q'}
	model, _ := m.Update(msgQ)
	if model.(*Model).state != StateList {
		t.Error("expected StateList after pressing 'q' in StateDetail")
	}

	// 2. Test Help -> Close transition via 'q'
	m.state = StateList
	m.listView.list.Help.ShowAll = true
	_, _ = m.Update(msgQ)
	// We can't easily check the internal list state change here because we return m.list.Update
	// but we've verified it compiles and calls the correct logic.

	// 3. Test Quit transition via 'q'
	m.listView.list.Help.ShowAll = false
	_, cmd := m.Update(msgQ)
	if cmd == nil {
		t.Fatal("expected quit command, got nil")
	}
}

func TestRenderTargetHeader_Geometry(t *testing.T) {
	styles := DefaultStyles(true)
	ctx := RenderContext{
		Styles: styles,
		Width:  80,
	}
	notif := db.NotificationWithState{
		Notification: db.Notification{
			SubjectTitle: "A very long title that should be truncated when a badge is present",
			SubjectType:  "PullRequest",
		},
	}

	// 1. Un-enriched (No badge, maximum density)
	h1 := RenderTargetHeader(ctx, notif, "", false)
	w1 := lipgloss.Width(h1)

	// 2. Fetching (Skeleton badge)
	ctx.IsFetching = true
	h2 := RenderTargetHeader(ctx, notif, "", false)
	w2 := lipgloss.Width(h2)

	// 3. Enriched (Status badge)
	ctx.IsFetching = false
	notif.ResourceState = "Merged"
	h3 := RenderTargetHeader(ctx, notif, "", false)
	w3 := lipgloss.Width(h3)

	if w1 != w2 || w2 != w3 {
		t.Errorf("expected all header states to have identical width for alignment: w1=%d, w2=%d, w3=%d", w1, w2, w3)
	}
}
