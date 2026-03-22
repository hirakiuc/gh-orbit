package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	"github.com/hirakiuc/gh-orbit/internal/triage"
	"github.com/stretchr/testify/assert"
)

func TestModel_RenderDetailView_SubState(t *testing.T) {
	m := newTestModel(t)
	m.width = 100
	m.height = 50

	tests := []struct {
		name             string
		resourceState    string
		resourceSubState string
		expectedContains []string
	}{
		{
			name:             "PR Approved",
			resourceState:    "Open",
			resourceSubState: "APPROVED",
			expectedContains: []string{"Open", "APPROVED"},
		},
		{
			name:             "Issue Completed",
			resourceState:    "Closed",
			resourceSubState: "COMPLETED",
			expectedContains: []string{"Closed", "COMPLETED"},
		},
		{
			name:             "Issue Not Planned",
			resourceState:    "Closed",
			resourceSubState: "NOT_PLANNED",
			expectedContains: []string{"Closed", "NOT PLANNED"}, // Underscore replaced by space
		},
		{
			name:             "Discussion Resolved",
			resourceState:    "Closed",
			resourceSubState: "RESOLVED",
			expectedContains: []string{"Closed", "RESOLVED"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup notification
			notif := triage.NotificationWithState{
				Notification: triage.Notification{
					SubjectTitle:       "Test Subject",
					RepositoryFullName: "org/repo",
					AuthorLogin:        "author",
					ResourceState:      tt.resourceState,
					ResourceSubState:   tt.resourceSubState,
				},
			}

			// Add to list and select it
			m.listView.list.SetItems([]list.Item{item{notification: notif}})
			m.listView.list.Select(0)

			view := m.renderDetailView()
			plain := stripANSI(view)

			for _, expected := range tt.expectedContains {
				assert.True(t, strings.Contains(plain, expected), "Expected view to contain '%s', but got: %s", expected, plain)
			}
		})
	}
}
