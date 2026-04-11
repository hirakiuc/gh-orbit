package tui

import (
	"context"
	"log/slog"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/api"
	"github.com/hirakiuc/gh-orbit/internal/triage"
	"github.com/hirakiuc/gh-orbit/internal/types"
)

var _ = slog.LevelInfo

// MarkReadByID marks a notification as read using only its ID.
func (m *Model) MarkReadByID(id string, read bool) tea.Cmd {
	// 1. Update master copy
	for idx, n := range m.allNotifications {
		if n.GitHubID == id {
			m.allNotifications[idx].IsReadLocally = read
			break
		}
	}

	m.applyFilters()

	// 2. Persistent Local & Remote Update via Traffic Controller
	return m.traffic.Submit(api.PriorityUser, func(ctx context.Context) tea.Msg {
		err := m.db.MarkReadLocally(ctx, id, read)
		if err != nil {
			m.logger.ErrorContext(ctx, "failed to update local read state", "error", err)
		}

		if read {
			err = m.client.MarkThreadAsRead(ctx, id)
			if err != nil {
				m.logger.ErrorContext(ctx, "failed to mark thread as read on GitHub", "error", err)
			}
		}
		return actionCompleteMsg{}
	})
}

// setPriorityByID updates the priority of a notification using only its ID.
func (m *Model) setPriorityByID(id string, priority int) tea.Cmd {
	return m.traffic.Submit(api.PriorityUser, func(ctx context.Context) tea.Msg {
		err := m.db.SetPriority(ctx, id, priority)
		if err != nil {
			return types.ErrMsg{Err: err}
		}

		// Reload to reflect state
		notifs, err := m.db.ListNotifications(ctx)
		if err != nil {
			return types.ErrMsg{Err: err}
		}

		toast := "Priority cleared"
		switch priority {
		case 1:
			toast = "Priority set to Low"
		case 2:
			toast = "Priority set to Medium"
		case 3:
			toast = "Priority set to High"
		}

		return priorityUpdatedMsg{notifications: notifs, toast: toast}
	})
}

const EnrichmentChunkSize = 10

// enrichItems triggers background enrichment for a specific set of notifications.
func (m *Model) enrichItems(toEnrich []triage.NotificationWithState, force bool) tea.Cmd {
	if len(toEnrich) == 0 {
		return nil
	}

	// 1. Mark as inflight
	now := time.Now()
	for _, n := range toEnrich {
		m.inflightEnrichments[n.GitHubID] = now
	}

	// 2. Split into Batch (has node_id) and Discovery (missing node_id) groups
	var batch []triage.NotificationWithState
	var discovery []triage.NotificationWithState

	for _, n := range toEnrich {
		if n.SubjectNodeID != "" {
			batch = append(batch, n)
		} else {
			discovery = append(discovery, n)
		}
	}

	var cmds []tea.Cmd

	// Handle Discovery items (one-by-one to get node_id)
	// We use PriorityEnrich for proactive discovery to avoid blocking user actions
	for _, n := range discovery {
		id, url, st := n.GitHubID, n.SubjectURL, n.SubjectType
		cmds = append(cmds, m.traffic.Submit(api.PriorityEnrich, func(ctx context.Context) tea.Msg {
			res, err := m.enrich.FetchDetail(ctx, url, string(st), force)
			if err != nil {
				return types.ErrMsg{Err: err}
			}
			// Atomic DB update including nodeID
			if err := m.db.EnrichNotification(ctx, id, res.SubjectNodeID, res.Body, res.Author, res.HTMLURL, res.ResourceState, res.ResourceSubState); err != nil {
				return types.ErrMsg{Err: err}
			}
			return detailLoadedMsg{
				GitHubID:         id,
				SubjectNodeID:    res.SubjectNodeID,
				Body:             res.Body,
				Author:           res.Author,
				HTMLURL:          res.HTMLURL,
				ResourceState:    res.ResourceState,
				ResourceSubState: res.ResourceSubState,
			}
		}))
	}

	// Handle Batch items (GraphQL batch fetch)
	for i := 0; i < len(batch); i += EnrichmentChunkSize {
		end := i + EnrichmentChunkSize
		if end > len(batch) {
			end = len(batch)
		}
		chunk := batch[i:end]

		cmds = append(cmds, m.traffic.Submit(api.PriorityEnrich, func(ctx context.Context) tea.Msg {
			results := m.enrich.FetchHybridBatch(ctx, chunk, force)
			return enrichmentBatchCompleteMsg{Results: results}
		}))
	}

	return tea.Batch(cmds...)
}

func (m *Model) MarkRead(i item) tea.Cmd {
	return m.MarkReadByID(i.notification.GitHubID, true)
}

func (m *Model) ToggleRead(i item) tea.Cmd {
	return m.MarkReadByID(i.notification.GitHubID, !i.notification.IsReadLocally)
}

func (m *Model) FetchDetailCmd(id, u string, subjectType triage.SubjectType, force bool) tea.Cmd {
	return m.traffic.Submit(api.PriorityUser, func(ctx context.Context) tea.Msg {
		res, err := m.enrich.FetchDetail(ctx, u, string(subjectType), force)
		if err != nil {
			return types.ErrMsg{Err: err}
		}

		// Update database with granular enrich method
		err = m.db.EnrichNotification(ctx, id, res.SubjectNodeID, res.Body, res.Author, res.HTMLURL, res.ResourceState, res.ResourceSubState)
		if err != nil {
			return types.ErrMsg{Err: err}
		}

		return detailLoadedMsg{
			GitHubID:         id,
			SubjectNodeID:    res.SubjectNodeID,
			Body:             res.Body,
			Author:           res.Author,
			HTMLURL:          res.HTMLURL,
			ResourceState:    res.ResourceState,
			ResourceSubState: res.ResourceSubState,
		}
	})
}
