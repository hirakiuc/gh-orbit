package tui

import (
	"fmt"
	"io"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/dustin/go-humanize"
	"github.com/hirakiuc/gh-orbit/internal/db"
)

type item struct {
	notification db.NotificationWithState
}

func (i item) Title() string       { return i.notification.SubjectTitle }
func (i item) Description() string { return i.notification.RepositoryFullName }
func (i item) FilterValue() string {
	return i.notification.SubjectTitle + " " + i.notification.RepositoryFullName
}

type itemDelegate struct {
	styles Styles
}

func newItemDelegate(s Styles) itemDelegate {
	return itemDelegate{styles: s}
}

func (d itemDelegate) Height() int                               { return 2 }
func (d itemDelegate) Spacing() int                              { return 0 }
func (d itemDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd { return nil }

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

func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(item)
	if !ok {
		return
	}

	isSelected := index == m.Index()

	// 1. Selection Indicator
	indicator := "  "
	if isSelected {
		indicator = d.styles.Cursor.Render("▌ ")
	}

	// 2. Unread Status Dot
	statusDot := "  "
	if !i.notification.IsReadLocally {
		statusDot = d.styles.Unread.Render("• ")
	}

	// 3. Type Icon
	typeIcon := "•"
	switch i.notification.SubjectType {
	case "PullRequest":
		typeIcon = ""
	case "Issue":
		typeIcon = ""
	case "Discussion":
		typeIcon = ""
	}
	// Fallback if Nerd Font is not available (common Unicode)
	if !strings.ContainsAny(typeIcon, "") {
		switch i.notification.SubjectType {
		case "PullRequest":
			typeIcon = "PR"
		case "Issue":
			typeIcon = "#"
		case "Discussion":
			typeIcon = "D"
		}
	}
	typeIconStr := d.styles.IconContainer.Render(typeIcon)

	// 4. Reason Icon
	reasonIcon := "  "
	if si, ok := reasonIcons[i.notification.Reason]; ok {
		icon := si.icon
		if !strings.ContainsAny(icon, "") {
			icon = si.fallback
		}
		reasonIcon = si.style(d.styles).Inherit(d.styles.IconContainer).Render(icon)
	}

	title := i.notification.SubjectTitle
	if isSelected {
		title = d.styles.SelectedTitle.Render(title)
	}

	// Stable Layout: [Selection] [Status] [Type] [Reason] [Index] [Title]
	str := fmt.Sprintf("%s%s%s%s%d. %s", indicator, statusDot, typeIconStr, reasonIcon, index+1, title)

	// Add priority indicator
	priority := ""
	switch i.notification.Priority {
	case 3:
		priority = d.styles.PriorityHigh.Render(" [!!!]")
	case 2:
		priority = d.styles.PriorityMed.Render(" [!!]")
	case 1:
		priority = d.styles.PriorityLow.Render(" [!]")
	}

	if priority != "" {
		str += priority
	}

	// Meta info: repository and relative time
	relTime := humanize.Time(i.notification.UpdatedAt)
	description := fmt.Sprintf("%s • %s", i.notification.RepositoryFullName, relTime)
	if isSelected {
		description = d.styles.SelectedDescription.Render(description)
	}

	_, _ = fmt.Fprintf(w, "%s\n    %s", str, description)
}
