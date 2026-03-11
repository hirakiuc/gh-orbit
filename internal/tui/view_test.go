package tui

import (
	"testing"

	"github.com/hirakiuc/gh-orbit/internal/types"
	"github.com/stretchr/testify/assert"
)

// Note: stripANSI is already defined in test_utils.go

func TestRenderNotificationRow_States(t *testing.T) {
	ctx := RenderContext{
		Styles: DefaultStyles(true),
		Width:  100,
	}
	
	notif := types.NotificationWithState{
		Notification: types.Notification{
			SubjectType: "PullRequest",
			SubjectTitle: "Title",
			SubjectURL: "https://github.com/o/r/pull/123",
			GitHubID: "123",
			ResourceState: "MERGED",
		},
	}
	
	// Test normal
	out := RenderNotificationRow(ctx, notif)
	assert.Contains(t, stripANSI(out), "Title")
	assert.Contains(t, stripANSI(out), "MERGED")
	
	// Test selected
	ctx.IsSelected = true
	outSelected := RenderNotificationRow(ctx, notif)
	assert.Contains(t, stripANSI(outSelected), "▌")
	
	// Test high priority with narrow width
	const testWidth = 100
	ctx.Width = testWidth
	notif.Priority = 3
	notif.SubjectTitle = "Very Long Title That Should Be Truncated"
	outPriority := RenderNotificationRow(ctx, notif)

	plain := stripANSI(outPriority)
	// Log for diagnostic visibility in CI if it fails
	t.Logf("Actual row width: %d, Content: [%s]", len(plain), plain)

	assert.Contains(t, plain, "[!!!]", "Priority badge must be visible")
	assert.LessOrEqual(t, len(plain), testWidth, "Row must not exceed available width")
}

func TestRenderHeader(t *testing.T) {
	m := newTestModel(t)
	m.width = 100
	view := m.renderHeader()
	assert.NotEmpty(t, view)
	assert.Contains(t, stripANSI(view), "Inbox")
}

func TestRenderFooter(t *testing.T) {
	m := newTestModel(t)
	m.width = 100
	view := m.renderFooter()
	assert.NotEmpty(t, view)
}

func TestRenderMarkdown(t *testing.T) {
	m := newTestModel(t)
	content := "# Hello\nWorld"
	rendered := m.renderMarkdown(content)
	assert.NotEmpty(t, rendered)
}
