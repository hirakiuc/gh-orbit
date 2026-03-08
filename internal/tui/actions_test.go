package tui

import (
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

func TestExtractNumberFromURL(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{"https://api.github.com/repos/owner/repo/pulls/123", "123"},
		{"https://api.github.com/repos/owner/repo/issues/456", "456"},
		{"", ""},
		{"invalid-url", ""},
		{"https://github.com/owner/repo/pull/789", "789"},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			assert.Equal(t, tt.expected, extractNumberFromURL(tt.url))
		})
	}
}

func TestExtractTagFromURL(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{"https://api.github.com/repos/owner/repo/releases/v1.0.0", "v1.0.0"},
		{"https://api.github.com/repos/owner/repo/releases/12345", "12345"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			assert.Equal(t, tt.expected, extractTagFromURL(tt.url))
		})
	}
}

func TestIsValidGitHubURL(t *testing.T) {
	tests := []struct {
		url      string
		expected bool
	}{
		{"https://github.com/owner/repo", true},
		{"https://api.github.com/repos/owner/repo", true},
		{"https://gist.github.com/user/id", true},
		{"https://google.com", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			assert.Equal(t, tt.expected, isValidGitHubURL(tt.url))
		})
	}
}

func TestModel_ViewItem(t *testing.T) {
	tests := []struct {
		name        string
		subjectType string
		subjectURL  string
		htmlURL     string
		expectedMsg string
	}{
		{
			name:        "PullRequest",
			subjectType: "PullRequest",
			subjectURL:  "https://api.github.com/repos/o/r/pulls/1",
			expectedMsg: "Opening PR...",
		},
		{
			name:        "Issue",
			subjectType: "Issue",
			subjectURL:  "https://api.github.com/repos/o/r/issues/1",
			expectedMsg: "Opening issue...",
		},
		{
			name:        "Release",
			subjectType: "Release",
			subjectURL:  "https://api.github.com/repos/o/r/releases/v1",
			expectedMsg: "Opening release...",
		},
		{
			name:        "Other",
			subjectType: "Discussion",
			htmlURL:     "https://github.com/o/r/discussions/1",
			expectedMsg: "Opening in browser...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel(t)
			mockTraffic := m.traffic.(*mocks.MockTrafficController)
			mockTraffic.EXPECT().Submit(mock.Anything, mock.Anything).Return(func() tea.Msg { return nil }).Maybe()

			notif := types.NotificationWithState{
				Notification: types.Notification{
					SubjectType:        tt.subjectType,
					SubjectURL:         tt.subjectURL,
					HTMLURL:            tt.htmlURL,
					RepositoryFullName: "o/r",
					GitHubID:           "id",
				},
			}
			i := item{notification: notif}
			m.listView.list.SetItems([]list.Item{i})
			m.allNotifications = []types.NotificationWithState{notif}
			
			// We don't need to execute the command, just check the toast message
			_ = m.ViewItem(i)
			assert.Equal(t, tt.expectedMsg, m.ui.toastMessage)
		})
	}
}

func TestModel_ToggleRead(t *testing.T) {
	m := newTestModel(t)
	notif := types.NotificationWithState{
		Notification: types.Notification{GitHubID: "id"},
		OrbitState:   types.OrbitState{IsReadLocally: false},
	}
	i := item{notification: notif}
	m.listView.list.SetItems([]list.Item{i})
	m.allNotifications = []types.NotificationWithState{notif}

	mockTraffic := m.traffic.(*mocks.MockTrafficController)
	mockTraffic.EXPECT().Submit(mock.Anything, mock.Anything).RunAndReturn(func(priority int, fn types.TaskFunc) tea.Cmd {
		return func() tea.Msg { return fn(context.Background()) }
	}).Once()
	// Noise
	mockTraffic.EXPECT().Submit(0, mock.Anything).Return(func() tea.Msg { return nil }).Maybe()

	mockRepo := m.db.(*mocks.MockRepository)
	mockRepo.EXPECT().MarkReadLocally(mock.Anything, "id", true).Return(nil).Once()
	mockClient := m.client.(*mocks.MockGitHubClient)
	mockClient.EXPECT().MarkThreadAsRead(mock.Anything, "id").Return(nil).Once()

	cmd := m.ToggleRead(i)
	require.NotNil(t, cmd)
	_ = executeCmd(cmd)

	assert.True(t, m.allNotifications[0].IsReadLocally)
	assert.Equal(t, "Marked as read", m.ui.toastMessage)
}

func TestModel_FetchDetailCmd(t *testing.T) {
	m := newTestModel(t)
	
	mockTraffic := m.traffic.(*mocks.MockTrafficController)
	mockTraffic.EXPECT().Submit(mock.Anything, mock.Anything).RunAndReturn(func(priority int, fn types.TaskFunc) tea.Cmd {
		return func() tea.Msg { return fn(context.Background()) }
	}).Once()
	// Noise
	mockTraffic.EXPECT().Submit(0, mock.Anything).Return(func() tea.Msg { return nil }).Maybe()

	mockEnricher := m.enrich.(*mocks.MockEnricher)
	mockEnricher.EXPECT().FetchDetail(mock.Anything, "url", "type").Return(types.EnrichmentResult{
		Body: "body",
		Author: "author",
	}, nil).Once()

	mockRepo := m.db.(*mocks.MockRepository)
	mockRepo.EXPECT().EnrichNotification(mock.Anything, "id", "body", "author", mock.Anything, mock.Anything).Return(nil).Once()

	cmd := m.FetchDetailCmd("id", "url", "type")
	require.NotNil(t, cmd)
	msg := executeCmd(cmd)
	
	require.IsType(t, detailLoadedMsg{}, msg)
	dMsg := msg.(detailLoadedMsg)
	assert.Equal(t, "id", dMsg.GitHubID)
	assert.Equal(t, "body", dMsg.Body)
}

func TestModel_CheckoutPR(t *testing.T) {
	m := newTestModel(t)
	notif := types.NotificationWithState{
		Notification: types.Notification{
			GitHubID:           "id",
			SubjectURL:         "https://api.github.com/repos/o/r/pulls/123",
			RepositoryFullName: "o/r",
			SubjectType:        "PullRequest",
		},
	}
	i := item{notification: notif}
	m.listView.list.SetItems([]list.Item{i})
	m.listView.list.Select(0)
	m.allNotifications = []types.NotificationWithState{notif}

	mockTraffic := m.traffic.(*mocks.MockTrafficController)
	mockTraffic.EXPECT().Submit(mock.Anything, mock.Anything).Return(func() tea.Msg { return nil }).Maybe()

	// We can't easily execute gh checkout in tests, but we can verify it doesn't panic
	// and that it returns an ExecProcess command.
	cmd := m.CheckoutPR("o/r", "123")
	require.NotNil(t, cmd)
	
	// Execute the command (it will try to run 'gh pr checkout')
	// Since 'gh' might not be in the environment or might fail, we don't strictly verify execution here.
}
