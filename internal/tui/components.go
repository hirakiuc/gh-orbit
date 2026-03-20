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
	const selectionIndicatorWidth = 2
	styles := ctx.Styles

	// 1. Selection Indicator
	indicator := "  "
	if ctx.IsSelected {
		indicator = styles.Cursor.Render("▌ ")
	}

	// 2. Icon Selection
	icon := "  "
	switch n.SubjectType {
	case "PullRequest":
		icon = " "
	case "Issue":
		icon = " "
	case "Discussion":
		icon = " "
	case "Release":
		icon = " "
	}

	// 3. Unread Indicator
	unread := renderUnreadIndicator(styles, n.IsReadLocally)

	// 4. Resource ID Extraction
	id := ""
	if lastIdx := strings.LastIndex(n.SubjectURL, "/"); lastIdx != -1 {
		id = "#" + n.SubjectURL[lastIdx+1:]
	}
	idStr := styles.SelectedDescription.Render(id)

	// 5. Status Badge Logic
	badge := ""
	if ctx.IsFetching {
		badge = styles.StateSkeleton.Render(" ◌ FETCH ")
	} else if n.ResourceState != "" {
		s := strings.ToUpper(n.ResourceState)
		switch s {
		case "OPEN":
			badge = styles.StateOpen.Render("  OPEN ")
		case "CLOSED":
			badge = styles.StateClosed.Render("  CLOSED ")
		case "MERGED":
			badge = styles.StateMerged.Render("  MERGED ")
		case "DRAFT":
			badge = styles.StateDraft.Render("  DRAFT ")
		default:
			badge = styles.StateSkeleton.UnsetBlink().Render(fmt.Sprintf(" %s ", s))
		}
	} else {
		// Placeholder for empty ResourceState to maintain consistent column alignment
		badge = styles.StateSkeleton.Render(" ◌ PEND  ")
	}

	// 6. Priority Badge
	priorityBadge := ""
	switch n.Priority {
	case 3:
		priorityBadge = styles.PriorityHigh.Render(" [!!!]")
	case 2:
		priorityBadge = styles.PriorityMed.Render(" [!!]")
	case 1:
		priorityBadge = styles.PriorityLow.Render(" [!]")
	}

	// 7. Dynamic Width Calculation (Subtract-from-Total)
	// We sum the visual width of all fixed components.
	// Note: We add a small safety buffer (5 cells) to account for multi-byte glyphs (Nerd Fonts)
	// which may render as 2 cells in some environments despite lipgloss.Width() reporting 1.
	fixedWidth := selectionIndicatorWidth + lipgloss.Width(icon) + lipgloss.Width(unread) + lipgloss.Width(idStr) + 7 // +2 for spaces, +5 safety
	if badge != "" {
		fixedWidth += lipgloss.Width(badge) + 1
	}
	if priorityBadge != "" {
		fixedWidth += lipgloss.Width(priorityBadge)
	}

	availableTitleWidth := ctx.Width - fixedWidth
	if availableTitleWidth < minTitleWidth {
		availableTitleWidth = minTitleWidth
	}

	// 8. Title Styling & Truncation
	titleStr := n.SubjectTitle
	titleStyle := styles.Unread
	if n.IsReadLocally {
		titleStyle = styles.SelectedDescription
	}
	if ctx.IsSelected {
		titleStyle = styles.SelectedTitle
	}
	title := titleStyle.Width(availableTitleWidth).MaxWidth(availableTitleWidth).Render(titleStr)

	// 9. Assembly
	// We use exact spacing to ensure width remains within ctx.Width
	return fmt.Sprintf("%s%s%s%s%s%s%s", indicator, icon, unread, badge, title, idStr, priorityBadge)
}

func renderUnreadIndicator(styles Styles, isRead bool) string {
	if isRead {
		return "  "
	}
	return styles.Unread.Render("• ")
}
