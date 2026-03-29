package tui

import (
	"fmt"
	"io"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/triage"
)

type item struct {
	notification triage.NotificationWithState
}

func (i item) Title() string       { return i.notification.SubjectTitle }
func (i item) Description() string { return i.notification.RepositoryFullName }
func (i item) FilterValue() string {
	return i.notification.SubjectTitle + " " + i.notification.RepositoryFullName + " " + i.notification.ResourceState
}

type itemDelegate struct {
	styles     Styles
	keys       KeyMap
	IsFetching bool
}

func newItemDelegate(s Styles, k KeyMap) itemDelegate {
	return itemDelegate{styles: s, keys: k}
}

func (d itemDelegate) Height() int                               { return 1 }
func (d itemDelegate) Spacing() int                              { return 0 }
func (d itemDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd { return nil }

func (d itemDelegate) ShortHelp() []key.Binding {
	return d.keys.ShortHelp()
}

func (d itemDelegate) FullHelp() [][]key.Binding {
	return d.keys.FullHelp()
}

func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(item)
	if !ok {
		return
	}

	isSelected := index == m.Index()

	// Target Identity Row (Indicator + Icon + Unread + Badge + Repo + Divider + Title + #ID + Time + Priority)
	ctx := RenderContext{
		Styles:     d.styles,
		Width:      m.Width(),
		IsFetching: isSelected && d.IsFetching,
		IsSelected: isSelected,
	}
	str := RenderNotificationRow(ctx, i.notification)

	_, _ = fmt.Fprint(w, str)
}
