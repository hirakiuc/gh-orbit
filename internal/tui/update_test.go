package tui

import (
	"bytes"
	"context"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/mocks"
	"github.com/hirakiuc/gh-orbit/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestModel_UpdateList(t *testing.T) {
	m := newTestModel(t)
	m.listView.activeTab = TabAll
	m.listView.list.SetSize(100, 20)

	notifs := []types.NotificationWithState{
		{Notification: types.Notification{GitHubID: "1", SubjectTitle: "T1"}},
	}

	mockTraffic := m.traffic.(*mocks.MockTrafficController)
	// Noise handler (Enrich on load)
	mockTraffic.EXPECT().Submit(0, mock.Anything).RunAndReturn(func(priority int, fn types.TaskFunc) tea.Cmd {
		return func() tea.Msg { return nil }
	}).Maybe()

	// Setup via proper Msg
	raw, _ := m.Update(notificationsLoadedMsg{notifications: notifs})
	m = raw.(*Model)
	
	require.NotEmpty(t, m.listView.list.Items())
	m.listView.list.Select(0)

	// 1. Test Space key on item (Detail transition)
	msgSpace := tea.KeyPressMsg{Code: ' ', Text: " "}
	
	// Expect ToggleDetail call (PriorityUser=2)
	mockTraffic.EXPECT().Submit(2, mock.Anything).RunAndReturn(func(priority int, fn types.TaskFunc) tea.Cmd {
		return func() tea.Msg { return nil }
	}).Once()

	updatedModel, _ := m.Update(msgSpace)
	assert.Equal(t, StateDetail, updatedModel.(*Model).state)
}

func TestModel_EnrichViewport(t *testing.T) {
	m := newTestModel(t)
	m.listView.activeTab = TabAll
	m.listView.list.SetSize(100, 20)
	
	notifs := []types.NotificationWithState{
		{Notification: types.Notification{GitHubID: "1", SubjectTitle: "T1", SubjectURL: "u1"}},
	}

	mockTraffic := m.traffic.(*mocks.MockTrafficController)
	// Expect exactly TWO calls: one on load, one on explicit call
	mockTraffic.EXPECT().Submit(0, mock.Anything).RunAndReturn(func(priority int, fn types.TaskFunc) tea.Cmd {
		return func() tea.Msg { return nil }
	}).Twice()

	raw, _ := m.Update(notificationsLoadedMsg{notifications: notifs})
	m = raw.(*Model)

	cmd := m.enrichViewport()
	require.NotNil(t, cmd)
	_ = cmd()
}

func TestItemDelegate_Render(t *testing.T) {
	styles := DefaultStyles(true)
	d := newItemDelegate(styles, DefaultKeyMap())
	
	notif := types.NotificationWithState{
		Notification: types.Notification{SubjectTitle: "Title", RepositoryFullName: "owner/repo"},
	}
	i := item{notification: notif}
	
	l := list.New([]list.Item{i}, d, 100, 10)
	
	buf := new(bytes.Buffer)
	d.Render(buf, l, 0, i)
	
	assert.Contains(t, buf.String(), "Title")
	assert.Contains(t, buf.String(), "owner/repo")
}

func TestItem_DelegateMethods(t *testing.T) {
	notif := types.NotificationWithState{
		Notification: types.Notification{
			SubjectTitle:       "Title",
			RepositoryFullName: "o/r",
			ResourceState:      "open",
		},
	}
	i := item{notification: notif}
	
	assert.Equal(t, "Title", i.Title())
	assert.Equal(t, "o/r", i.Description())
	assert.Equal(t, "Title o/r open", i.FilterValue())
}

func TestModel_ApplyFilters(t *testing.T) {
	m := newTestModel(t)
	notifs := []types.NotificationWithState{
		{Notification: types.Notification{GitHubID: "1"}, OrbitState: types.OrbitState{Status: "Triaged", Priority: 1}},
		{Notification: types.Notification{GitHubID: "2"}, OrbitState: types.OrbitState{IsReadLocally: false}},
		{Notification: types.Notification{GitHubID: "3"}, OrbitState: types.OrbitState{IsReadLocally: true}},
	}
	m.allNotifications = notifs
	
	// TabAll
	m.listView.activeTab = TabAll
	m.applyFilters()
	assert.Equal(t, 3, len(m.listView.list.Items()))
	
	// TabUnread
	m.listView.activeTab = TabUnread
	m.applyFilters()
	assert.Equal(t, 2, len(m.listView.list.Items())) // 1 and 2 are unread
	
	// TabTriaged
	m.listView.activeTab = TabTriaged
	m.applyFilters()
	assert.Equal(t, 1, len(m.listView.list.Items())) // only 1 has priority > 0
}

func TestModel_UpdateDetail(t *testing.T) {
	m := newTestModel(t)
	m.state = StateDetail
	notif := types.NotificationWithState{
		Notification: types.Notification{GitHubID: "1", SubjectTitle: "T1", HTMLURL: "https://github.com/o/r"},
	}
	m.listView.list.SetItems([]list.Item{item{notification: notif}})
	m.listView.list.Select(0)
	
	// 1. Back key (esc)
	msgEsc := tea.KeyPressMsg{Code: tea.KeyEsc}
	updated, _ := m.Update(msgEsc)
	assert.Equal(t, StateList, updated.(*Model).state)
	
	// 2. Open browser (enter)
	m.state = StateDetail
	msgEnter := tea.KeyPressMsg{Code: tea.KeyEnter, Text: "enter"}
	
	mockTraffic := m.traffic.(*mocks.MockTrafficController)
	mockTraffic.EXPECT().Submit(mock.Anything, mock.Anything).Return(func() tea.Msg { return nil }).Maybe()
	
	_, cmd := m.Update(msgEnter)
	_ = executeCmd(cmd)
	assert.Equal(t, "Opening in browser...", m.ui.toastMessage)
}

func TestModel_EnrichViewport_Empty(t *testing.T) {
	m := newTestModel(t)
	// No items -> nothing to enrich
	cmd := m.enrichViewport()
	assert.Nil(t, cmd)
}

func TestModel_SetPriority(t *testing.T) {
	m := newTestModel(t)
	notif := types.NotificationWithState{
		Notification: types.Notification{GitHubID: "id"},
		OrbitState:   types.OrbitState{Priority: 0},
	}
	i := item{notification: notif}
	m.listView.list.SetItems([]list.Item{i})
	m.listView.list.Select(0)
	m.allNotifications = []types.NotificationWithState{notif}

	mockTraffic := m.traffic.(*mocks.MockTrafficController)
	mockTraffic.EXPECT().Submit(mock.Anything, mock.Anything).RunAndReturn(func(priority int, fn types.TaskFunc) tea.Cmd {
		return func() tea.Msg { return fn(context.Background()) }
	}).Once()
	// Noise
	mockTraffic.EXPECT().Submit(0, mock.Anything).Return(func() tea.Msg { return nil }).Maybe()

	mockRepo := m.db.(*mocks.MockRepository)
	mockRepo.EXPECT().SetPriority(mock.Anything, "id", 1).Return(nil).Once()
	mockRepo.EXPECT().ListNotifications(mock.Anything).Return([]types.NotificationWithState{notif}, nil).Maybe()

	cmd := m.setPriority(1)
	require.NotNil(t, cmd)
	msg := executeCmd(cmd)
	
	require.IsType(t, priorityUpdatedMsg{}, msg)
	
	// Apply message to update model state (including toast)
	_, _ = m.Update(msg)
	
	assert.Equal(t, "Priority set to Low", m.ui.toastMessage)
}
