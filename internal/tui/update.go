package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/db"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "r":
			return m, m.syncNotifications()
		case "y":
			// Copy URL
			if i, ok := m.list.SelectedItem().(item); ok && i.notification.HTMLURL != "" {
				if isValidGitHubURL(i.notification.HTMLURL) {
					return m, tea.SetClipboard(i.notification.HTMLURL)
				}
				m.err = fmt.Errorf("refusing to copy untrusted URL: %s", i.notification.HTMLURL)
			}
		case "1", "2", "3":
			// Set priority
			if i, ok := m.list.SelectedItem().(item); ok {
				priority := int(msg.String()[0] - '0')
				i.notification.Priority = priority
				err := m.db.UpdateOrbitState(db.OrbitState{
					NotificationID: i.notification.GitHubID,
					Priority:       priority,
					Status:         i.notification.Status,
					IsReadLocally:  i.notification.IsReadLocally,
				})
				if err != nil {
					m.err = err
				}
				// Refresh list item
				m.list.SetItem(m.list.Index(), i)
			}
		}

	case tea.WindowSizeMsg:
		m.list.SetSize(msg.Width, msg.Height)

	case notificationsLoadedMsg:
		items := make([]list.Item, len(msg))
		for i, n := range msg {
			items[i] = item{notification: n}
		}
		cmds = append(cmds, m.list.SetItems(items))

	case errMsg:
		m.err = msg.err
	}

	// Only update list if it's not filtering
	if m.list.FilterState() != list.Filtering {
		m.list, cmd = m.list.Update(msg)
		cmds = append(cmds, cmd)
	} else {
		// Even if filtering, we need to pass the message to the list
		// but bubbles/v2/list handles it internally in its Update.
		m.list, cmd = m.list.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func isValidGitHubURL(url string) bool {
	return strings.HasPrefix(url, "https://github.com/") ||
		strings.HasPrefix(url, "https://api.github.com/")
}
