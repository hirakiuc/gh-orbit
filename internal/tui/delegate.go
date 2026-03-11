package tui

import (
	"fmt"
	"io"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/dustin/go-humanize"
	"github.com/hirakiuc/gh-orbit/internal/types"
)

type item struct {
	notification types.NotificationWithState
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

func (d itemDelegate) Height() int                               { return 2 }
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

	// 1. Target Identity Row (Indicator + Icon + Unread + Badge + Title + #ID + Priority)
	ctx := RenderContext{
		Styles:     d.styles,
		Width:      m.Width(),
		IsFetching: isSelected && d.IsFetching,
		IsSelected: isSelected,
	}
	str := RenderNotificationRow(ctx, i.notification)

	// 2. Meta info (line 2)
	relTime := humanize.Time(i.notification.UpdatedAt)
	description := fmt.Sprintf("%s • %s", i.notification.RepositoryFullName, relTime)
	if isSelected {
		description = d.styles.SelectedDescription.Render(description)
	}

	_, _ = fmt.Fprintf(w, "%s\n    %s", str, description)
}
