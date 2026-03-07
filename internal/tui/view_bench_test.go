package tui

import (
	"fmt"
	"testing"

	"github.com/hirakiuc/gh-orbit/internal/types"
)

func BenchmarkModel_View(b *testing.B) {
	m := newTestModel(b) 
	
	// Pre-populate many notifications
	notifs := make([]types.NotificationWithState, 1000)
	for i := 0; i < 1000; i++ {
		notifs[i] = types.NotificationWithState{
			Notification: types.Notification{
				GitHubID:     fmt.Sprintf("%d", i),
				SubjectTitle: "Benchmark notification item",
				SubjectType:  "PullRequest",
			},
		}
	}
	m.allNotifications = notifs
	m.applyFilters()
	m.ui.SetSize(100, 40)

	b.ResetTimer()
	for b.Loop() {
		_ = m.View()
	}
}
