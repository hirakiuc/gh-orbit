package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/hirakiuc/gh-orbit/internal/triage"
)

type RenderContext struct {
	Styles     Styles
	Width      int
	IsFetching bool
	IsSelected bool
}

// RenderNotificationRow provides a consistent, non-truncated row layout.
// Layout: [Indicator][Icon][Unread][Badge] [Title] [ID] [Priority]
func RenderNotificationRow(ctx RenderContext, n triage.NotificationWithState) string {
	const minTitleWidth = 10
	indicator := renderSelectionIndicator(ctx.Styles, ctx.IsSelected)
	icon := renderNotificationIcon(n.SubjectType)
	unread := renderUnreadIndicator(ctx.Styles, n.IsReadLocally)
	badge := renderResourceStateBadge(ctx, n.ResourceState)
	idStr := renderResourceID(ctx.Styles, n.SubjectURL)
	priority := renderPriorityBadge(ctx.Styles, n.Priority)
	titleWidth := calculateAvailableTitleWidth(ctx.Width, minTitleWidth, icon, unread, badge, idStr, priority)
	title := renderNotificationTitle(ctx, n, titleWidth)

	return fmt.Sprintf("%s%s%s%s%s%s%s", indicator, icon, unread, badge, title, idStr, priority)
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

func renderNotificationIcon(subjectType string) string {
	switch subjectType {
	case "PullRequest":
		return " "
	case "Issue":
		return " "
	case "Discussion":
		return " "
	case "Release":
		return " "
	default:
		return "  "
	}
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

func calculateAvailableTitleWidth(totalWidth, minTitleWidth int, icon, unread, badge, idStr, priority string) int {
	const selectionIndicatorWidth = 2
	const layoutSafetyBuffer = 7 // 2 spaces + 5 extra cells for multi-width glyphs.

	fixedWidth := selectionIndicatorWidth + lipgloss.Width(icon) + lipgloss.Width(unread) + lipgloss.Width(idStr) + layoutSafetyBuffer
	if badge != "" {
		fixedWidth += lipgloss.Width(badge) + 1
	}
	if priority != "" {
		fixedWidth += lipgloss.Width(priority)
	}

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
	return titleStyle.Width(width).MaxWidth(width).Render(n.SubjectTitle)
}
