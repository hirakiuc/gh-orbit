package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/db"
)

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		// Only handle custom keys if the list isn't filtering
		if m.list.FilterState() == list.Filtering {
			break
		}

		switch {
		case msg.String() == "ctrl+c" || msg.String() == "q":
			return m, tea.Quit
		case key.Matches(msg, m.keys.Sync):
			if m.syncing {
				return m, nil
			}
			m.syncing = true
			return m, tea.Batch(
				m.syncNotifications(),
				m.spinner.Tick,
			)
		case key.Matches(msg, m.keys.CopyURL):
			if i, ok := m.list.SelectedItem().(item); ok && i.notification.HTMLURL != "" {
				if isValidGitHubURL(i.notification.HTMLURL) {
					return m, tea.SetClipboard(i.notification.HTMLURL)
				}
				m.err = fmt.Errorf("refusing to copy untrusted URL: %s", i.notification.HTMLURL)
			}
		case key.Matches(msg, m.keys.OpenBrowser):
			if i, ok := m.list.SelectedItem().(item); ok && i.notification.HTMLURL != "" {
				m.status = "Opening browser..."
				return m, m.OpenBrowser(i.notification.HTMLURL)
			}
		case key.Matches(msg, m.keys.CheckoutPR):
			if i, ok := m.list.SelectedItem().(item); ok && i.notification.SubjectType == "PullRequest" {
				prNumber := extractNumberFromURL(i.notification.SubjectURL)
				if prNumber != "" {
					m.status = "Launching gh pr checkout..."
					return m, m.CheckoutPR(i.notification.RepositoryFullName, prNumber)
				}
				m.err = fmt.Errorf("could not extract PR number from URL: %s", i.notification.SubjectURL)
			}
		case key.Matches(msg, m.keys.ViewContextual):
			if i, ok := m.list.SelectedItem().(item); ok {
				return m, m.ViewItem(i)
			}
		case key.Matches(msg, m.keys.SetPriorityLow):
			m.setPriority(1)
		case key.Matches(msg, m.keys.SetPriorityMed):
			m.setPriority(2)
		case key.Matches(msg, m.keys.SetPriorityHigh):
			m.setPriority(3)
		}

	case tea.WindowSizeMsg:
		m.list.SetSize(msg.Width, msg.Height-1) // Leave space for footer

	case tea.BackgroundColorMsg:
		m.styles = DefaultStyles(msg.IsDark())
		m.list.Styles.Title = m.styles.Title
		m.list.SetDelegate(newItemDelegate(m.styles, m.keys))

	case notificationsLoadedMsg:
		items := make([]list.Item, len(msg))
		for i, n := range msg {
			items[i] = item{notification: n}
		}
		cmds = append(cmds, m.list.SetItems(items))

	case syncCompleteMsg:
		m.syncing = false
		items := make([]list.Item, len(msg))
		for i, n := range msg {
			items[i] = item{notification: n}
		}
		cmds = append(cmds, m.list.SetItems(items))

	case spinner.TickMsg:
		if m.syncing {
			m.spinner, cmd = m.spinner.Update(msg)
			cmds = append(cmds, cmd)
		}

	case errMsg:
		m.syncing = false
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

func (m *Model) setPriority(priority int) {
	if i, ok := m.list.SelectedItem().(item); ok {
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

func isValidGitHubURL(url string) bool {
	return strings.HasPrefix(url, "https://github.com/") ||
		strings.HasPrefix(url, "https://api.github.com/")
}
