package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/hirakiuc/gh-orbit/internal/types"
)

type RenderContext struct {
	Styles     Styles
	Width      int
	IsFetching bool
}

// RenderTargetHeader provides a consistent title row with Icons and Status badges.
func RenderTargetHeader(ctx RenderContext, n types.NotificationWithState, filter string, isSelected bool) string {
	styles := ctx.Styles

	// 1. Icon Selection
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

	// 2. Unread Indicator
	unread := renderUnreadIndicator(styles, n.IsReadLocally)

	// 3. Resource ID Extraction
	id := ""
	if lastIdx := strings.LastIndex(n.SubjectURL, "/"); lastIdx != -1 {
		id = "#" + n.SubjectURL[lastIdx+1:]
	}

	// 4. Title Styling (Read vs Unread)
	titleStr := n.SubjectTitle
	titleStyle := styles.Unread
	if n.IsReadLocally {
		titleStyle = styles.SelectedDescription // Use a dim style for read
	}
	if isSelected {
		titleStyle = styles.SelectedTitle
	}

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
			// Generic badge for other states
			badge = styles.StateSkeleton.UnsetBlink().Render(fmt.Sprintf(" %s ", s))
		}
	}

	// 6. Layout with truncation
	// fixed widths: icon (2), unread (2), id (~5)
	fixedWidth := 15
	if badge != "" {
		fixedWidth += lipgloss.Width(badge) + 1
	}

	availableTitleWidth := ctx.Width - fixedWidth
	if availableTitleWidth < 10 {
		availableTitleWidth = 10
	}

	title := titleStyle.Width(availableTitleWidth).MaxWidth(availableTitleWidth).Render(titleStr)
	idStr := styles.SelectedDescription.Render(id) // Use a subtle style for ID

	return fmt.Sprintf("%s%s%s %s %s", icon, unread, badge, title, idStr)
}

func renderUnreadIndicator(styles Styles, isRead bool) string {
	if isRead {
		return "  "
	}
	return styles.Unread.Render("• ")
}
