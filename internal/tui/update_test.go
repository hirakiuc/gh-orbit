package tui

import (
	"bytes"
	"testing"

	"charm.land/bubbles/v2/list"
	"github.com/hirakiuc/gh-orbit/internal/types"
	"github.com/stretchr/testify/assert"
)

/*
func TestModel_UpdateList(t *testing.T) {
	m := newTestModel(t)
	m.listView.activeTab = TabAll
	m.listView.list.SetSize(100, 20)

	notifs := []types.NotificationWithState{
		{Notification: types.Notification{GitHubID: "1", SubjectTitle: "T1"}},
	}

	// Setup via proper Msg
	raw, _ := m.Update(notificationsLoadedMsg{notifications: notifs})
	m = raw.(*Model)
	
	require.NotEmpty(t, m.listView.list.Items())
	m.listView.list.Select(0)

	// 1. Test Enter key on item (Detail transition)
	msgEnter := tea.KeyPressMsg{Text: "enter", Code: tea.KeyEnter}
	
	mockTraffic := m.traffic.(*mocks.MockTrafficController)
	mockTraffic.On("Submit", mock.Anything, mock.MatchedBy(func(interface{}) bool { return true })).Return(func() tea.Msg { return nil }).Maybe()

	updatedModel, _ := m.Update(msgEnter)
	assert.Equal(t, StateDetail, updatedModel.(*Model).state)
}

func TestModel_EnrichViewport(t *testing.T) {
	m := newTestModel(t)
	m.listView.activeTab = TabAll
	m.listView.list.SetSize(100, 20)
	
	notifs := []types.NotificationWithState{
		{Notification: types.Notification{GitHubID: "1", SubjectTitle: "T1", SubjectURL: "u1"}},
	}
	raw, _ := m.Update(notificationsLoadedMsg{notifications: notifs})
	m = raw.(*Model)

	mockTraffic := m.traffic.(*mocks.MockTrafficController)
	mockTraffic.On("Submit", mock.Anything, mock.Anything).Return(func() tea.Msg { return nil }).Once()

	cmd := m.enrichViewport()
	require.NotNil(t, cmd)
	_ = cmd()
}
*/

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
