package tui

import (
	"fmt"
	"io"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
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

	str := fmt.Sprintf("%d. %s", index+1, i.notification.SubjectTitle)

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

	_, _ = fmt.Fprintf(w, "%s\n  %s", str, i.notification.RepositoryFullName)
}
