package tui

import (
	"fmt"
	"io"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
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

func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(item)
	if !ok {
		return
	}

	isSelected := index == m.Index()

	// Indicator
	indicator := "  "
	if isSelected {
		indicator = d.styles.Cursor.Render("▌ ")
	}

	// Type icon with fallback
	icon := "•"
	switch i.notification.SubjectType {
	case "PullRequest":
		icon = "" // Nerd Font
	case "Issue":
		icon = "" // Nerd Font
	case "Discussion":
		icon = "" // Nerd Font
	}
	// Fallback if Nerd Font is not available (common Unicode)
	if !strings.ContainsAny(icon, "") {
		switch i.notification.SubjectType {
		case "PullRequest":
			icon = "PR"
		case "Issue":
			icon = "#"
		case "Discussion":
			icon = "D"
		}
	}

	title := i.notification.SubjectTitle
	if isSelected {
		title = d.styles.SelectedTitle.Render(title)
	}

	str := fmt.Sprintf("%s %s %d. %s", indicator, icon, index+1, title)

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
