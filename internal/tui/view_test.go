package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/hirakiuc/gh-orbit/internal/triage"
	"github.com/stretchr/testify/assert"
)

// Note: stripANSI is already defined in test_utils_test.go.

func TestRenderNotificationRow_States(t *testing.T) {
	ctx := RenderContext{
		Styles: DefaultStyles(true),
		Width:  100,
	}

	notif := triage.NotificationWithState{
		Notification: triage.Notification{
			SubjectType:        "PullRequest",
			SubjectTitle:       "Title",
			SubjectURL:         "https://github.com/o/r/pull/123",
			RepositoryFullName: "owner/repo",
			GitHubID:           "123",
			ResourceState:      "MERGED",
		},
	}

	// Test normal (Repo-First)
	out := RenderNotificationRow(ctx, notif)
	plain := stripANSI(out)
	assert.Contains(t, plain, "owner/repo")
	assert.Contains(t, plain, "│")
	assert.Contains(t, plain, "Title")
	assert.Contains(t, plain, "MERGED")

	// Verify order: repo before title
	repoIdx := strings.Index(plain, "owner/repo")
	titleIdx := strings.Index(plain, "Title")
	assert.Less(t, repoIdx, titleIdx, "Repo must come before Title")

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

	plain = stripANSI(outPriority)
	// Log for diagnostic visibility in CI if it fails
	t.Logf("Actual row width: %d, Content: [%s]", len(plain), plain)

	assert.Contains(t, plain, "[!!!]", "Priority badge must be visible")
	assert.LessOrEqual(t, len(plain), testWidth, "Row must not exceed available width")
}

func TestRenderNotificationRow_EmptyState(t *testing.T) {
	ctx := RenderContext{
		Styles: DefaultStyles(true),
		Width:  100,
	}

	notifOpen := triage.NotificationWithState{
		Notification: triage.Notification{
			SubjectType:        "PullRequest",
			SubjectTitle:       "Title",
			SubjectURL:         "https://github.com/o/r/pull/123",
			RepositoryFullName: "owner/repo",
			ResourceState:      "OPEN",
		},
	}

	notifEmpty := triage.NotificationWithState{
		Notification: triage.Notification{
			SubjectType:        "PullRequest",
			SubjectTitle:       "Title",
			SubjectURL:         "https://github.com/o/r/pull/124",
			RepositoryFullName: "owner/repo",
			ResourceState:      "", // Empty state should use placeholder
		},
	}

	outOpen := RenderNotificationRow(ctx, notifOpen)
	outEmpty := RenderNotificationRow(ctx, notifEmpty)

	plainOpen := stripANSI(outOpen)
	plainEmpty := stripANSI(outEmpty)

	t.Logf("Row with OPEN:  [%s]", plainOpen)
	t.Logf("Row with EMPTY: [%s]", plainEmpty)

	assert.Contains(t, plainEmpty, "PEND", "Should contain PEND placeholder")
	assert.Equal(t, len(plainOpen), len(plainEmpty), "Row lengths must be equal for consistent alignment")
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

	t.Run("Normal state", func(t *testing.T) {
		m.focusMode = "Inactive"
		view := m.renderFooter()
		assert.NotEmpty(t, view)
		assert.NotContains(t, stripANSI(view), "[DND]")
	})

	t.Run("Active Focus Mode", func(t *testing.T) {
		m.focusMode = "Active"
		view := m.renderFooter()
		assert.NotEmpty(t, view)
		assert.Contains(t, stripANSI(view), "[DND]")
	})
}

func TestRenderNotificationRow_LongRepo(t *testing.T) {
	ctx := RenderContext{
		Styles: DefaultStyles(true),
		Width:  100,
	}

	notif := triage.NotificationWithState{
		Notification: triage.Notification{
			SubjectType:        "PullRequest",
			SubjectTitle:       "Small Title",
			SubjectURL:         "https://github.com/o/r/pull/123",
			RepositoryFullName: "a-very-long-repository-name-that-exceeds-twenty-chars",
			GitHubID:           "123",
			ResourceState:      "OPEN",
		},
	}

	out := RenderNotificationRow(ctx, notif)
	plain := stripANSI(out)

	// Verify that the output is exactly 1 line
	assert.NotContains(t, out, "\n", "Row must not contain newlines")
	assert.Equal(t, 1, lipgloss.Height(out), "Row height must be exactly 1")

	// Verify truncation of repo name
	assert.Contains(t, plain, "a-very-long-repos...", "Repo name must be truncated with ellipsis")
}

func TestRenderMarkdown(t *testing.T) {
	m := newTestModel(t)
	content := "# Hello\nWorld"
	rendered := m.renderMarkdown(content)
	assert.NotEmpty(t, rendered)
}
