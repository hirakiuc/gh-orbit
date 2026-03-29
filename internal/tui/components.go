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

// RenderNotificationRow provides a consistent, high-density Repo-First row layout.
// Layout: [Indicator+Icon+Unread] [Badge] [Repo] │ [Title] [#ID] [Time] [Priority]
func RenderNotificationRow(ctx RenderContext, n triage.NotificationWithState) string {
	const (
		indicatorCellWidth = 6
		badgeWidth         = 12
		dividerWidth       = 3
		priorityWidth      = 6
	)

	// 1. Determine responsive columns
	idStr := ""
	idWidth := 0
	if ctx.Width >= 80 {
		idStr = renderResourceID(ctx.Styles, n.SubjectURL)
		idWidth = 10
	}

	timeStr := ""
	timeWidth := 0
	if ctx.Width >= 100 {
		timeStr = renderRelativeTime(ctx.Styles, n.UpdatedAt)
		timeWidth = 15
	}

	// 2. Calculate flexible space (30/70 ratio for Repo vs Title)
	fixedSpace := indicatorCellWidth + badgeWidth + dividerWidth + priorityWidth + idWidth + timeWidth
	flexibleSpace := ctx.Width - fixedSpace
	if flexibleSpace < 20 {
		flexibleSpace = 20
	}

	repoWidth := int(float64(flexibleSpace) * 0.3)
	if repoWidth < 10 {
		repoWidth = 10
	}
	titleWidth := flexibleSpace - repoWidth

	// 3. Prepare cells with fixed widths and strict truncation
	indicator := renderSelectionIndicator(ctx.Styles, ctx.IsSelected)
	icon := renderNotificationIcon(n.SubjectType)
	unread := renderUnreadIndicator(ctx.Styles, n.IsReadLocally)
	indicatorCell := renderCell(indicator+icon+unread, indicatorCellWidth, false)

	badge := renderCell(renderResourceStateBadge(ctx, n.ResourceState), badgeWidth, false)
	repo := renderCell(renderRepoColumn(ctx.Styles, n.RepositoryFullName, repoWidth), repoWidth, false)
	divider := renderCell(ctx.Styles.Separator.Render(" │ "), dividerWidth, false)
	title := renderCell(renderNotificationTitle(ctx, n, titleWidth), titleWidth, false)

	idCell := ""
	if idWidth > 0 {
		idCell = renderCell(idStr, idWidth, true)
	}

	timeCell := ""
	if timeWidth > 0 {
		timeCell = renderCell(timeStr, timeWidth, true)
	}

	priority := renderCell(renderPriorityBadge(ctx.Styles, n.Priority), priorityWidth, false)

	// 4. Join horizontally for a guaranteed 1-line layout
	return lipgloss.JoinHorizontal(
		lipgloss.Bottom,
		indicatorCell,
		badge,
		repo,
		divider,
		title,
		idCell,
		timeCell,
		priority,
	)
}

func renderCell(content string, width int, styleDefault bool) string {
	s := lipgloss.NewStyle().
		Width(width).
		MaxWidth(width).
		Height(1).
		MaxHeight(1).
		Padding(0, 0).
		Margin(0, 0)
	return s.Render(truncateToWidth(content, width))
}

func truncateToWidth(s string, w int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	// Note: lipgloss.Width handles multi-width characters correctly.
	if lipgloss.Width(s) <= w {
		return s
	}

	// Simple rune-based truncation if lipgloss.Width is too large
	runes := []rune(s)
	if len(runes) > w {
		return string(runes[:w-1]) + "…"
	}
	return s
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
	return relTime
}

func renderResourceID(styles Styles, subjectURL string) string {
	id := ""
	if lastIdx := strings.LastIndex(subjectURL, "/"); lastIdx != -1 {
		id = "#" + subjectURL[lastIdx+1:]
	}
	return id
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
