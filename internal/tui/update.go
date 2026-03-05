package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/api"
	"github.com/hirakiuc/gh-orbit/internal/db"
)

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	// 1. Handle state-independent messages
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.listView.list.SetSize(msg.Width, msg.Height-3)
		
		vpWidth := msg.Width - 4
		if vpWidth < 0 { vpWidth = 0 }
		m.detailView.viewport.SetWidth(vpWidth)
		
		vpHeight := msg.Height - 8
		if vpHeight < 0 { vpHeight = 0 }
		m.detailView.viewport.SetHeight(vpHeight)

		m.updateMarkdownRenderer()
		m.ui.SetSize(msg.Width, msg.Height)

	case tea.BackgroundColorMsg:
		m.isDark = msg.IsDark()
		m.styles = DefaultStyles(m.isDark)
		m.listView.list.Styles.Title = m.styles.Title
		m.listView.delegate = newItemDelegate(m.styles, m.keys)
		m.listView.list.SetDelegate(m.listView.delegate)
		m.updateMarkdownRenderer()
		m.ui.SetStyles(m.styles)

	case notificationsLoadedMsg:
		m.allNotifications = msg
		m.applyFilters()
		// Start initial heartbeat and enrichment
		cmds = append(cmds, m.enrichViewport())
		cmds = append(cmds, m.tickHeartbeat())

	case syncCompleteMsg:
		cmds = append(cmds, m.ui.SetSyncing(false))
		m.allNotifications = msg
		m.LastSyncAt = time.Now()
		m.applyFilters()
		// Schedule next heartbeat
		cmds = append(cmds, m.tickHeartbeat())

	case enrichmentCompleteMsg:
		m.allNotifications = msg
		m.applyFilters()
		// No new heartbeat here - this breaks the recursive loop

	case pollTickMsg:
		// Only process if ID matches current (Timer ID Tagging)
		if msg.ID == m.heartbeatID {
			if !m.ui.syncing {
				m.syncCounter++
				// Every 5th background poll is a "Cold" refresh to ensure eventual consistency
				force := (m.syncCounter%5 == 0)
				cmds = append(cmds, m.syncNotificationsWithForce(api.PrioritySync, force))
			} else {
				// If already busy, skip this tick but keep the pulse going
				cmds = append(cmds, m.tickHeartbeat())
			}
		}


	case clockTickMsg:
		if msg.ID == m.clockID {
			cmds = append(cmds, m.tickClock())
		}

	case viewportEnrichMsg:
		cmds = append(cmds, m.enrichViewport())

	case detailLoadedMsg:
		cmds = append(cmds, m.ui.SetFetching(false))
		// Update master copy
		for idx, n := range m.allNotifications {
			if n.GitHubID == msg.GitHubID {
				m.allNotifications[idx].Body = msg.Body
				m.allNotifications[idx].AuthorLogin = msg.Author
				m.allNotifications[idx].HTMLURL = msg.HTMLURL
				m.allNotifications[idx].ResourceState = msg.ResourceState
				m.allNotifications[idx].IsEnriched = true
				break
			}
		}
		// If we are in detail view and this is the active item, update viewport
		if m.state == StateDetail {
			if i, ok := m.listView.list.SelectedItem().(item); ok && i.notification.GitHubID == msg.GitHubID {
				m.detailView.activeDetail = m.renderMarkdown(msg.Body)
				m.detailView.viewport.SetContent(m.detailView.activeDetail)
			}
		}
		m.applyFilters()

	case spinner.TickMsg:
		m.ui, cmd = m.ui.Update(msg)
		cmds = append(cmds, cmd)

	case clearStatusMsg:
		m.ui, cmd = m.ui.Update(msg)
		cmds = append(cmds, cmd)

	case errMsg:
		cmds = append(cmds, m.ui.SetSyncing(false))
		cmds = append(cmds, m.ui.SetFetching(false))
		m.err = msg.err
	}

	// Sync fetching state to delegate for skeleton UI
	if m.listView.delegate.IsFetching != m.ui.fetchingDetail {
		m.listView.delegate.IsFetching = m.ui.fetchingDetail
		m.listView.list.SetDelegate(m.listView.delegate)
	}

	// 2. Handle state-dependent messages (Router Pattern)
	var stateCmd tea.Cmd
	oldIndex := m.listView.list.Index()
	switch m.state {
	case StateDetail:
		_, stateCmd = m.updateDetail(msg)
	case StateList:
		_, stateCmd = m.updateList(msg)
	}
	cmds = append(cmds, stateCmd)

	// Trigger debounced enrichment if viewport might have changed
	if m.state == StateList && m.listView.list.Index() != oldIndex {
		duration := time.Duration(m.config.Enrichment.DebounceMS) * time.Millisecond
		cmds = append(cmds, tea.Tick(duration, func(_ time.Time) tea.Msg {
			return viewportEnrichMsg{}
		}))
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) updateDetail(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyPressMsg); ok {
		if key.Matches(msg, m.keys.ToggleDetail) || msg.String() == "esc" || msg.String() == "q" {
			m.state = StateList
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.detailView.viewport, cmd = m.detailView.viewport.Update(msg)
	return m, cmd
}

func (m *Model) updateList(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		// Only handle custom keys if the list isn't filtering
		if m.listView.list.FilterState() == list.Filtering {
			break
		}

		switch {
		case msg.String() == "ctrl+c":
			return m, tea.Quit
		case msg.String() == "q":
			if m.listView.list.Help.ShowAll {
				// Close help overlay by sending "?" to the list
				m.listView.list, cmd = m.listView.list.Update(tea.KeyPressMsg{
					Text: "?",
					Code: '?',
				})
				return m, cmd
			}
			return m, tea.Quit
		case key.Matches(msg, m.keys.Sync):
			if m.ui.syncing {
				return m, nil
			}
			return m, tea.Batch(
				m.ui.SetSyncing(true),
				m.syncNotificationsWithForce(api.PriorityUser, true), // Manual is always Cold
			)
		case key.Matches(msg, m.keys.ToggleDetail):
			if i, ok := m.listView.list.SelectedItem().(item); ok {
				m.state = StateDetail
				if !i.notification.IsEnriched {
					return m, tea.Batch(
						m.ui.SetFetching(true),
						m.enrich.GetEnrichmentCmd(
							i.notification.GitHubID,
							i.notification.SubjectURL,
							i.notification.SubjectType,
							func(res api.EnrichmentResult) tea.Msg {
								return detailLoadedMsg{
									GitHubID:      i.notification.GitHubID,
									Body:          res.Body,
									Author:        res.Author,
									HTMLURL:       res.HTMLURL,
									ResourceState: res.ResourceState,
								}
							},
							func(err error) tea.Msg { return errMsg{err: err} },
						),
					)
				}
				// Prepare viewport content
				m.detailView.activeDetail = m.renderMarkdown(i.notification.Body)
				m.detailView.viewport.SetContent(m.detailView.activeDetail)
				return m, nil
			}
			return m, nil
		case key.Matches(msg, m.keys.CopyURL):
			if i, ok := m.listView.list.SelectedItem().(item); ok && i.notification.HTMLURL != "" {
				if isValidGitHubURL(i.notification.HTMLURL) {
					return m, tea.Batch(
						tea.SetClipboard(i.notification.HTMLURL),
						m.ui.SetToast("Copied URL to clipboard"),
					)
				}
				m.err = fmt.Errorf("refusing to copy untrusted URL: %s", i.notification.HTMLURL)
			}
		case key.Matches(msg, m.keys.ToggleRead):
			if i, ok := m.listView.list.SelectedItem().(item); ok {
				cmds = append(cmds, m.ToggleRead(i))
			}
		case key.Matches(msg, m.keys.NextTab):
			m.listView.activeTab = (m.listView.activeTab + 1) % 4
			m.applyFilters()
		case key.Matches(msg, m.keys.PrevTab):
			m.listView.activeTab = (m.listView.activeTab - 1 + 4) % 4
			m.applyFilters()
		case key.Matches(msg, m.keys.OpenBrowser):
			if i, ok := m.listView.list.SelectedItem().(item); ok && i.notification.HTMLURL != "" {
				return m, tea.Batch(m.OpenBrowser(i.notification.HTMLURL), m.ui.SetToast("Opening browser..."))
			}
		case key.Matches(msg, m.keys.CheckoutPR):
			if i, ok := m.listView.list.SelectedItem().(item); ok && i.notification.SubjectType == "PullRequest" {
				prNumber := extractNumberFromURL(i.notification.SubjectURL)
				if prNumber != "" {
					return m, tea.Batch(m.CheckoutPR(i.notification.RepositoryFullName, prNumber), m.ui.SetToast("Launching gh pr checkout..."))
				}
				m.err = fmt.Errorf("could not extract PR number from URL: %s", i.notification.SubjectURL)
			}
		case key.Matches(msg, m.keys.ViewContextual):
			if i, ok := m.listView.list.SelectedItem().(item); ok {
				return m, m.ViewItem(i)
			}
		case key.Matches(msg, m.keys.SetPriorityLow):
			return m, m.setPriority(1)
		case key.Matches(msg, m.keys.SetPriorityMed):
			return m, m.setPriority(2)
		case key.Matches(msg, m.keys.SetPriorityHigh):
			return m, m.setPriority(3)
		case key.Matches(msg, m.keys.ClearPriority):
			return m, m.setPriority(0)
		case key.Matches(msg, m.keys.FilterPR):
			return m, m.toggleResourceFilter("PullRequest", "PRs")
		case key.Matches(msg, m.keys.FilterIssue):
			return m, m.toggleResourceFilter("Issue", "Issues")
		case key.Matches(msg, m.keys.FilterDiscussion):
			return m, m.toggleResourceFilter("Discussion", "Discussions")
		}
	}

	m.listView.list, cmd = m.listView.list.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m *Model) enrichViewport() tea.Cmd {
	// Identify visible items
	start, end := m.listView.list.Paginator.GetSliceBounds(len(m.listView.list.Items()))
	if start < 0 || end > len(m.listView.list.Items()) || start >= end {
		return nil
	}
	visible := m.listView.list.Items()[start:end]

	var toEnrich []db.NotificationWithState
	for _, li := range visible {
		if i, ok := li.(item); ok {
			if !i.notification.IsEnriched {
				toEnrich = append(toEnrich, i.notification)
			}
		}
	}

	if len(toEnrich) == 0 {
		return nil
	}

	// Bulk background enrichment using the Traffic Controller
	return m.traffic.Submit(api.PriorityEnrich, func(ctx context.Context) tea.Msg {
		// Use GraphQL Batching for efficient viewport enrichment
		results := m.enrich.FetchHybridBatch(ctx, toEnrich)
		if len(results) == 0 {
			return nil
		}

		// Since multiple items were updated in DB, we trigger a refresh of the local list state.
		// We use the new enrichmentCompleteMsg to avoid recursive heartbeat storms.
		notifs, err := m.db.ListNotifications()
		if err != nil {
			return errMsg{err: err}
		}
		return enrichmentCompleteMsg(notifs)
	})
}

func (m *Model) setPriority(priority int) tea.Cmd {
	if i, ok := m.listView.list.SelectedItem().(item); ok {
		// Toggle logic: if already at this priority, reset to 0
		if i.notification.Priority == priority {
			priority = 0
		}

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

		// Update feedback
		msg := "Priority cleared"
		switch priority {
		case 1:
			msg = "Priority set to Low"
		case 2:
			msg = "Priority set to Medium"
		case 3:
			msg = "Priority set to High"
		}

		// Update allNotifications master copy
		for idx, n := range m.allNotifications {
			if n.GitHubID == i.notification.GitHubID {
				m.allNotifications[idx].Priority = priority
				break
			}
		}
		m.applyFilters()

		return m.ui.SetToast(msg)
	}
	return nil
}

func (m *Model) toggleResourceFilter(filterType, label string) tea.Cmd {
	if m.listView.resourceFilter == filterType {
		m.listView.resourceFilter = ""
		m.ui.SetResourceFilter("")
		m.applyFilters()
		return m.ui.SetToast("Filter cleared")
	}

	m.listView.resourceFilter = filterType
	m.ui.SetResourceFilter(label)
	m.applyFilters()
	return m.ui.SetToast(fmt.Sprintf("Filtering by %s", label))
}

func (m *Model) applyFilters() {
	// 1. Store currently selected ID
	var selectedID string
	if i, ok := m.listView.list.SelectedItem().(item); ok {
		selectedID = i.notification.GitHubID
	}

	// 2. Filter allNotifications
	var filtered []list.Item
	for _, n := range m.allNotifications {
		keep := false

		// Apply Tab Filter
		switch m.listView.activeTab {
		case TabInbox:
			if !n.IsReadLocally || n.Priority > 0 { keep = true }
		case TabUnread:
			if !n.IsReadLocally { keep = true }
		case TabTriaged:
			if n.Priority > 0 { keep = true }
		case TabAll:
			keep = true
		}

		// Apply Resource Type Filter (if active)
		if keep && m.listView.resourceFilter != "" {
			if n.SubjectType != m.listView.resourceFilter {
				keep = false
			}
		}

		if keep {
			filtered = append(filtered, item{notification: n})
		}
	}

	// 3. Update list items
	m.listView.list.SetItems(filtered)

	// 4. Restore selection
	if selectedID != "" {
		for index, li := range m.listView.list.Items() {
			if i, ok := li.(item); ok && i.notification.GitHubID == selectedID {
				m.listView.list.Select(index)
				break
			}
		}
	}
}

func isValidGitHubURL(url string) bool {
	return strings.HasPrefix(url, "https://github.com/") ||
		strings.HasPrefix(url, "https://api.github.com/")
}
