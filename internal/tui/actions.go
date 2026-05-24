package tui

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/api"
	"github.com/hirakiuc/gh-orbit/internal/triage"
	"github.com/hirakiuc/gh-orbit/internal/types"
)

var _ = slog.LevelInfo

func enrichmentBatchScope(chunk []triage.NotificationWithState) string {
	ids := make([]string, 0, len(chunk))
	for _, n := range chunk {
		ids = append(ids, n.GitHubID)
	}
	return "enrich:batch:" + strings.Join(ids, ",")
}

// MarkReadByID marks a notification as read using only its ID.
func (m *Model) MarkReadByID(id string, read bool) tea.Cmd {
	// 1. Update master copy
	originalRead := read
	found := false
	for idx, n := range m.allNotifications {
		if n.GitHubID == id {
			originalRead = m.allNotifications[idx].IsReadLocally
			m.allNotifications[idx].IsReadLocally = read
			found = true
			break
		}
	}

	m.applyFilters()

	// 2. Persistent Local & Remote Update via Traffic Controller
	return m.submitTask("read:"+id, 0, api.PriorityUser, func(ctx context.Context) any {
		err := m.db.MarkReadLocally(ctx, id, read)
		if err != nil {
			m.logger.ErrorContext(ctx, "failed to update local read state", "error", err)
			notifs, reloadErr := m.db.ListNotifications(ctx)
			if reloadErr != nil {
				if found {
					for idx, n := range m.allNotifications {
						if n.GitHubID == id {
							m.allNotifications[idx].IsReadLocally = originalRead
							break
						}
					}
					m.applyFilters()
				}
				return types.ErrMsg{Err: fmt.Errorf("reload notifications after local read failure: %w (original error: %v)", reloadErr, err)}
			}
			return markReadReconciledMsg{
				notifications: notifs,
				status:        markReadReconcileLocalFailure,
				err:           err,
				toast:         "Failed to update read state",
			}
		}

		// Connected mode delegates the remote read mutation to the engine-backed
		// repository path, so the direct GitHub client is optional here.
		status := markReadReconcileSuccess
		var resultErr error
		toast := ""
		if read && m.client != nil {
			err = m.client.MarkThreadAsRead(ctx, id)
			if err != nil {
				m.logger.ErrorContext(ctx, "failed to mark thread as read on GitHub", "error", err)
				status = markReadReconcileRemoteFailure
				resultErr = err
				toast = "Marked read locally; GitHub sync failed"
			}
		}

		notifs, err := m.db.ListNotifications(ctx)
		if err != nil {
			return types.ErrMsg{Err: err}
		}

		return markReadReconciledMsg{
			notifications: notifs,
			status:        status,
			err:           resultErr,
			toast:         toast,
		}
	})
}

// setPriorityByID updates the priority of a notification using only its ID.
func (m *Model) setPriorityByID(id string, priority int) tea.Cmd {
	return m.submitTask("priority:"+id, 0, api.PriorityUser, func(ctx context.Context) any {
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
		cmds = append(cmds, m.submitTask("enrich:detail:"+id, 0, api.PriorityEnrich, func(ctx context.Context) any {
			res, err := m.enrich.FetchDetail(ctx, url, string(st), force)
			if err != nil {
				return types.ErrMsg{Err: err}
			}
			if err := m.enrich.PersistFetchedDetail(ctx, id, url, res); err != nil {
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

		cmds = append(cmds, m.submitTask(enrichmentBatchScope(chunk), 0, api.PriorityEnrich, func(ctx context.Context) any {
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
	return m.submitTask("detail:view", 0, api.PriorityUser, func(ctx context.Context) any {
		res, err := m.enrich.FetchDetail(ctx, u, string(subjectType), force)
		if err != nil {
			return types.ErrMsg{Err: err}
		}

		if err := m.enrich.PersistFetchedDetail(ctx, id, u, res); err != nil {
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
