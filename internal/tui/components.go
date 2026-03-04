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
	Styles     Styles
	Width      int
	IsFetching bool
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

	// 3. Status Badge (Smooth Transition Layout)
	badgeWidth := 12
	statusBadge := ""

	if ctx.IsFetching {
		// State 1: Fetching (Skeleton)
		statusBadge = ctx.Styles.StateSkeleton.Width(badgeWidth).Align(lipgloss.Left).Render(" [LOADING] ")
	} else if n.ResourceState != "" {
		// State 2: Enriched
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
		badgeText := fmt.Sprintf("%s[%s]", icon, n.ResourceState)
		statusBadge = style.Width(badgeWidth).Align(lipgloss.Left).Render(badgeText)
	} else {
		// State 3: Un-enriched (Empty Placeholder of fixed width)
		statusBadge = lipgloss.NewStyle().Width(badgeWidth).Render("")
	}

	// 4. Title + #ID (with Adaptive Truncation)
	title := n.SubjectTitle
	number := extractNumberFromURL(n.SubjectURL)
	if number != "" {
		title = fmt.Sprintf("%s #%s", title, number)
	}

	// Always calculate available width assuming badge is present (for perfect alignment)
	occupied := 6 + badgeWidth + 1
	avail := ctx.Width - occupied
	if avail < 10 {
		avail = 10
	}
	title = truncateString(title, avail)

	// Highlight matches
	if filter != "" {
		title = highlightMatches(ctx, title, filter)
	} else if isSelected {
		title = ctx.Styles.SelectedTitle.Render(title)
	}

	// 5. Reason Badge
	reasonBadge := ""
	if si, ok := reasonIcons[n.Reason]; ok {
		style := si.style(ctx.Styles)
		badgeText := strings.ToUpper(strings.ReplaceAll(n.Reason, "_", " "))
		reasonBadge = style.
			Padding(0, 1).
			Render(badgeText)
	}

	return fmt.Sprintf("%s%s %s %s %s", iconStr, statusDot, statusBadge, title, reasonBadge)
}

func highlightMatches(ctx RenderContext, text, filter string) string {
	matches := fuzzy.Find(filter, []string{text})
	if len(matches) == 0 {
		return text
	}

	// sahilm/fuzzy returns indices for the first match
	return lipgloss.StyleRunes(text, matches[0].MatchedIndexes, ctx.Styles.FuzzyMatch, ctx.Styles.SelectedTitle)
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen < 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
