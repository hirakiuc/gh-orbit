package tui

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
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
			if m.syncing {
				return m, nil
			}
			m.syncing = true
			return m, tea.Batch(
				m.syncNotifications(),
				m.spinner.Tick,
			)
		case "y":
			// Copy URL
			if i, ok := m.list.SelectedItem().(item); ok && i.notification.HTMLURL != "" {
				if isValidGitHubURL(i.notification.HTMLURL) {
					return m, tea.SetClipboard(i.notification.HTMLURL)
				}
				m.err = fmt.Errorf("refusing to copy untrusted URL: %s", i.notification.HTMLURL)
			}
		case "enter":
			if i, ok := m.list.SelectedItem().(item); ok && i.notification.HTMLURL != "" {
				m.status = "Opening browser..."
				return m, m.OpenBrowser(i.notification.HTMLURL)
			}
		case "c":
			if i, ok := m.list.SelectedItem().(item); ok && i.notification.SubjectType == "PullRequest" {
				prNumber := extractNumberFromURL(i.notification.SubjectURL)
				if prNumber != "" {
					m.status = "Launching gh pr checkout..."
					return m, m.CheckoutPR(i.notification.RepositoryFullName, prNumber)
				}
				m.err = fmt.Errorf("could not extract PR number from URL: %s", i.notification.SubjectURL)
			}
		case "v":
			if i, ok := m.list.SelectedItem().(item); ok && i.notification.SubjectType == "PullRequest" {
				prNumber := extractNumberFromURL(i.notification.SubjectURL)
				if prNumber != "" {
					m.status = "Opening PR in browser..."
					return m, m.ViewPRWeb(i.notification.RepositoryFullName, prNumber)
				}
				m.err = fmt.Errorf("could not extract PR number from URL: %s", i.notification.SubjectURL)
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
		m.list.SetSize(msg.Width, msg.Height-1) // Leave space for footer

	case tea.BackgroundColorMsg:
		m.styles = DefaultStyles(msg.IsDark())
		m.list.Styles.Title = m.styles.Title
		m.list.SetDelegate(newItemDelegate(m.styles))

	case notificationsLoadedMsg:
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

func isValidGitHubURL(url string) bool {
	return strings.HasPrefix(url, "https://github.com/") ||
		strings.HasPrefix(url, "https://api.github.com/")
}

func extractNumberFromURL(u string) string {
	if u == "" {
		return ""
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return ""
	}

	// Example: https://api.github.com/repos/owner/repo/pulls/123
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segments) > 0 {
		last := segments[len(segments)-1]
		if regexp.MustCompile(`^[0-9]+$`).MatchString(last) {
			return last
		}
	}
	return ""
}
