package tui

import (
	"fmt"
	"strings"

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

	// 2. Resource ID Extraction
	id := ""
	if lastIdx := strings.LastIndex(n.SubjectURL, "/"); lastIdx != -1 {
		id = "#" + n.SubjectURL[lastIdx+1:]
	}

	// 3. Title Styling (Read vs Unread)
	titleStr := n.SubjectTitle
	titleStyle := styles.Unread
	if n.IsReadLocally {
		titleStyle = styles.SelectedDescription // Use a dim style for read
	}
	if isSelected {
		titleStyle = styles.SelectedTitle
	}

	// 4. Status Badge Logic
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
			badge = styles.StateSkeleton.Copy().UnsetBlink().Render(fmt.Sprintf(" %s ", s))
		}
	}

	// 5. Layout with truncation
	// indicator is 2, icon is 2, id is ~5, badge is ~10. Total fixed ~20.
	fixedWidth := 25
	availableTitleWidth := ctx.Width - fixedWidth
	if availableTitleWidth < 10 {
		availableTitleWidth = 10
	}

	title := titleStyle.Width(availableTitleWidth).MaxWidth(availableTitleWidth).Render(titleStr)
	idStr := styles.SelectedDescription.Render(id) // Use a subtle style for ID

	return fmt.Sprintf("%s%s %s %s", icon, title, idStr, badge)
}
