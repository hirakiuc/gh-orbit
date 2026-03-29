package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/dustin/go-humanize"
	"github.com/hirakiuc/gh-orbit/internal/triage"
)

type RenderContext struct {
	Styles     Styles
	Width      int
	IsFetching bool
	IsSelected bool
}

// RenderNotificationRow provides a consistent, non-truncated row layout.
// RenderNotificationRow provides a consistent, high-density Repo-First row layout.
// Layout: [Indicator][Icon][Unread][Badge] [Repo] │ [Title] [ID] [Time] [Priority]
func RenderNotificationRow(ctx RenderContext, n triage.NotificationWithState) string {
	const minTitleWidth = 10
	const repoWidth = 20

	indicator := renderSelectionIndicator(ctx.Styles, ctx.IsSelected)
	icon := renderNotificationIcon(n.SubjectType)
	unread := renderUnreadIndicator(ctx.Styles, n.IsReadLocally)
	badge := renderResourceStateBadge(ctx, n.ResourceState)
	repo := renderRepoColumn(ctx.Styles, n.RepositoryFullName, repoWidth)
	divider := ctx.Styles.Separator.Render(" │ ")

	idStr := ""
	timeStr := ""
	if ctx.Width >= 80 {
		idStr = " " + renderResourceID(ctx.Styles, n.SubjectURL)
		timeStr = " " + renderRelativeTime(ctx.Styles, n.UpdatedAt)
	}

	priority := renderPriorityBadge(ctx.Styles, n.Priority)

	titleWidth := calculateAvailableTitleWidth(ctx.Width, minTitleWidth, icon, unread, badge, repo, divider, idStr, timeStr, priority)
	title := renderNotificationTitle(ctx, n, titleWidth)

	return fmt.Sprintf("%s%s%s%s%s%s%s%s%s%s", indicator, icon, unread, badge, repo, divider, title, idStr, timeStr, priority)
}

func renderUnreadIndicator(styles Styles, isRead bool) string {
	if isRead {
		return "  "
	}
	return styles.Unread.Render("• ")
}

func renderSelectionIndicator(styles Styles, isSelected bool) string {
	if isSelected {
		return styles.Cursor.Render("▌ ")
	}
	return "  "
}

func renderNotificationIcon(subjectType triage.SubjectType) string {
	switch subjectType {
	case triage.SubjectPullRequest:
		return " "
	case triage.SubjectIssue:
		return " "
	case triage.SubjectDiscussion:
		return " "
	case triage.SubjectRelease:
		return " "
	default:
		return "  "
	}
}

func renderRepoColumn(styles Styles, repoFullName string, width int) string {
	// Manual truncation to ensure it fits in exactly 1 line
	runes := []rune(repoFullName)
	if len(runes) > width {
		repoFullName = string(runes[:width-3]) + "..."
	}

	return styles.SelectedDescription.
		Width(width).
		MaxWidth(width).
		Render(repoFullName)
}

func renderRelativeTime(styles Styles, updatedAt time.Time) string {
	relTime := humanize.Time(updatedAt)
	return styles.SelectedDescription.Render(relTime)
}

func renderResourceID(styles Styles, subjectURL string) string {
	id := ""
	if lastIdx := strings.LastIndex(subjectURL, "/"); lastIdx != -1 {
		id = "#" + subjectURL[lastIdx+1:]
	}
	return styles.SelectedDescription.Render(id)
}

func renderResourceStateBadge(ctx RenderContext, resourceState string) string {
	styles := ctx.Styles
	if ctx.IsFetching {
		return styles.StateSkeleton.Render(" ◌ FETCH ")
	}
	if resourceState == "" {
		return styles.StateSkeleton.Render(" ◌ PEND  ")
	}

	s := strings.ToUpper(resourceState)
	switch s {
	case "OPEN":
		return styles.StateOpen.Render("  OPEN ")
	case "CLOSED":
		return styles.StateClosed.Render("  CLOSED ")
	case "MERGED":
		return styles.StateMerged.Render("  MERGED ")
	case "DRAFT":
		return styles.StateDraft.Render("  DRAFT ")
	default:
		return styles.StateSkeleton.UnsetBlink().Render(fmt.Sprintf(" %s ", s))
	}
}

func renderPriorityBadge(styles Styles, priority int) string {
	switch priority {
	case 3:
		return styles.PriorityHigh.Render(" [!!!]")
	case 2:
		return styles.PriorityMed.Render(" [!!]")
	case 1:
		return styles.PriorityLow.Render(" [!]")
	default:
		return ""
	}
}

func calculateAvailableTitleWidth(totalWidth, minTitleWidth int, icon, unread, badge, repo, divider, idStr, timeStr, priority string) int {
	const selectionIndicatorWidth = 2
	const layoutSafetyBuffer = 10 // Increased to handle multi-width icons and padding.

	fixedWidth := selectionIndicatorWidth +
		lipgloss.Width(icon) +
		lipgloss.Width(unread) +
		lipgloss.Width(badge) +
		lipgloss.Width(repo) +
		lipgloss.Width(divider) +
		lipgloss.Width(idStr) +
		lipgloss.Width(timeStr) +
		lipgloss.Width(priority) +
		layoutSafetyBuffer

	availableTitleWidth := totalWidth - fixedWidth
	if availableTitleWidth < minTitleWidth {
		return minTitleWidth
	}
	return availableTitleWidth
}

func renderNotificationTitle(ctx RenderContext, n triage.NotificationWithState, width int) string {
	titleStyle := ctx.Styles.Unread
	if n.IsReadLocally {
		titleStyle = ctx.Styles.SelectedDescription
	}
	if ctx.IsSelected {
		titleStyle = ctx.Styles.SelectedTitle
	}

	// Sanitize title: remove newlines and tabs to maintain single-line density
	cleanTitle := strings.ReplaceAll(n.SubjectTitle, "\n", " ")
	cleanTitle = strings.ReplaceAll(cleanTitle, "\r", "")
	cleanTitle = strings.ReplaceAll(cleanTitle, "\t", " ")
	cleanTitle = strings.TrimSpace(cleanTitle)

	// Manual truncation to ensure it fits in exactly 1 line
	// Note: We use runes to handle multi-width characters more safely,
	// although Title is usually ASCII.
	runes := []rune(cleanTitle)
	if len(runes) > width {
		cleanTitle = string(runes[:width-3]) + "..."
	}

	return titleStyle.Width(width).MaxWidth(width).Render(cleanTitle)
}
