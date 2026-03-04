package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/hirakiuc/gh-orbit/internal/db"
	"github.com/sahilm/fuzzy"
)

// RenderContext holds shared configuration for component rendering.
type RenderContext struct {
	Styles Styles
	Width  int
}

type semanticIcon struct {
	icon     string
	fallback string
	style    func(s Styles) lipgloss.Style
}

var reasonIcons = map[string]semanticIcon{
	"mention":          {"", "@", func(s Styles) lipgloss.Style { return s.Mention }},
	"review_requested": {"", "R", func(s Styles) lipgloss.Style { return s.ReviewRequested }},
	"author":           {"", "A", func(s Styles) lipgloss.Style { return s.Member }},
	"assign":           {"", "G", func(s Styles) lipgloss.Style { return s.Assign }},
	"security_alert":   {"", "!", func(s Styles) lipgloss.Style { return s.ActionRequired }},
	"comment":          {"", "C", func(s Styles) lipgloss.Style { return s.Subscribed }},
	"manual":           {"", "M", func(s Styles) lipgloss.Style { return s.Subscribed }},
	"subscribed":       {"", "S", func(s Styles) lipgloss.Style { return s.Subscribed }},
	"state_change":     {"", "X", func(s Styles) lipgloss.Style { return s.Subscribed }},
}

// RenderTargetHeader provides a unified header for notifications in both list and detail views.
func RenderTargetHeader(ctx RenderContext, n db.NotificationWithState, filter string, isSelected bool) string {
	// 1. Type Icon
	typeIcon := "•"
	switch n.SubjectType {
	case "PullRequest":
		typeIcon = ""
	case "Issue":
		typeIcon = ""
	case "Discussion":
		typeIcon = ""
	case "Commit":
		typeIcon = ""
	case "Release":
		typeIcon = ""
	}
	if !strings.ContainsAny(typeIcon, "") {
		switch n.SubjectType {
		case "PullRequest":
			typeIcon = "PR"
		case "Issue":
			typeIcon = "#"
		case "Discussion":
			typeIcon = "D"
		default:
			typeIcon = "•"
		}
	}
	iconStr := ctx.Styles.IconContainer.Render(typeIcon)

	// 2. Unread status
	statusDot := " "
	if !n.IsReadLocally {
		statusDot = ctx.Styles.Unread.Render("•")
	}

	// 3. Title + #ID
	title := n.SubjectTitle
	number := extractNumberFromURL(n.SubjectURL)
	if number != "" {
		title = fmt.Sprintf("%s #%s", title, number)
	}

	// Highlight matches
	if filter != "" {
		title = highlightMatches(ctx, title, filter)
	} else if isSelected {
		title = ctx.Styles.SelectedTitle.Render(title)
	}

	// 4. Resource Status Badge (Draft, Open, Merged, etc.)
	statusBadge := ""
	if n.ResourceState != "" {
		style := ctx.Styles.StateDraft
		icon := ""
		switch n.ResourceState {
		case "Open":
			style = ctx.Styles.StateOpen
			icon = " "
		case "Merged":
			style = ctx.Styles.StateMerged
			icon = " "
		case "Closed":
			style = ctx.Styles.StateClosed
			icon = " "
		case "Draft":
			style = ctx.Styles.StateDraft
			icon = " "
		}
		
		// Fixed-width container (width: 12) to prevent title jumping
		badgeText := fmt.Sprintf("%s[%s]", icon, n.ResourceState)
		statusBadge = style.Width(12).Align(lipgloss.Left).Render(badgeText)
	} else {
		// Empty space to maintain layout stability
		statusBadge = lipgloss.NewStyle().Width(12).Render("")
	}

	// 5. Reason Badge
	badge := ""
	if si, ok := reasonIcons[n.Reason]; ok {
		style := si.style(ctx.Styles)
		badgeText := strings.ToUpper(strings.ReplaceAll(n.Reason, "_", " "))
		badge = style.
			Padding(0, 1).
			Render(badgeText)
	}

	return fmt.Sprintf("%s%s %s %s %s", iconStr, statusDot, statusBadge, title, badge)
}

func highlightMatches(ctx RenderContext, text, filter string) string {
	matches := fuzzy.Find(filter, []string{text})
	if len(matches) == 0 {
		return text
	}

	// sahilm/fuzzy returns indices for the first match
	return lipgloss.StyleRunes(text, matches[0].MatchedIndexes, ctx.Styles.FuzzyMatch, ctx.Styles.SelectedTitle)
}
