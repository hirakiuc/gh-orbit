package tui

import (
	"fmt"
	"io"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/dustin/go-humanize"
	"github.com/hirakiuc/gh-orbit/internal/db"
	"github.com/sahilm/fuzzy"
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
	keys   KeyMap
}

func newItemDelegate(s Styles, k KeyMap) itemDelegate {
	return itemDelegate{styles: s, keys: k}
}

func (d itemDelegate) Height() int                               { return 2 }
func (d itemDelegate) Spacing() int                              { return 0 }
func (d itemDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd { return nil }

func (d itemDelegate) ShortHelp() []key.Binding {
	return d.keys.ShortHelp()
}

func (d itemDelegate) FullHelp() [][]key.Binding {
	return d.keys.FullHelp()
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

	// 2. Target Identity Header (Icon + Title + #ID + Badge)
	header := d.renderTargetHeader(i.notification, m.FilterValue(), isSelected)

	// 3. Status/Priority
	str := fmt.Sprintf("%s%s", indicator, header)

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

	// 4. Meta info (line 2)
	relTime := humanize.Time(i.notification.UpdatedAt)
	description := fmt.Sprintf("%s • %s", i.notification.RepositoryFullName, relTime)
	if isSelected {
		description = d.styles.SelectedDescription.Render(description)
	}

	_, _ = fmt.Fprintf(w, "%s\n    %s", str, description)
}

func (d itemDelegate) renderTargetHeader(n db.NotificationWithState, filter string, isSelected bool) string {
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
	iconStr := d.styles.IconContainer.Render(typeIcon)

	// 2. Unread status
	statusDot := " "
	if !n.IsReadLocally {
		statusDot = d.styles.Unread.Render("•")
	}

	// 3. Title + #ID
	title := n.SubjectTitle
	number := extractNumberFromURL(n.SubjectURL)
	if number != "" {
		title = fmt.Sprintf("%s #%s", title, number)
	}

	// Highlight matches
	if filter != "" {
		title = d.highlightMatches(title, filter)
	} else if isSelected {
		title = d.styles.SelectedTitle.Render(title)
	}

	// 4. Reason Badge
	badge := ""
	if si, ok := reasonIcons[n.Reason]; ok {
		style := si.style(d.styles)
		badgeText := strings.ToUpper(strings.ReplaceAll(n.Reason, "_", " "))
		badge = style.
			Padding(0, 1).
			Render(badgeText)
	}

	return fmt.Sprintf("%s%s %s  %s", iconStr, statusDot, title, badge)
}

func (d itemDelegate) highlightMatches(text, filter string) string {
	matches := fuzzy.Find(filter, []string{text})
	if len(matches) == 0 {
		return text
	}

	// sahilm/fuzzy returns indices for the first match
	return lipgloss.StyleRunes(text, matches[0].MatchedIndexes, d.styles.FuzzyMatch, d.styles.SelectedTitle)
}
