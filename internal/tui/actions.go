package tui

import (
	"context"
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
	for idx, n := range m.allNotifications {
		if n.GitHubID == id {
			m.allNotifications[idx].IsReadLocally = read
			m.allNotifications[idx].IsHandledLocally = read
			break
		}
	}

	m.applyFilters()

	// 2. Persistent Local & Remote Update via Traffic Controller
	return m.submitMutationTask("read:"+id, func(ctx context.Context) (mutationAppliedMsg, error) {
		result, err := m.backend.MarkRead(ctx, id, read)
		if err != nil {
			return mutationAppliedMsg{}, err
		}

		return mutationAppliedMsg{
			notifications: result.Notifications,
			err:           result.Err,
			toast:         result.Toast,
		}, nil
	})
}

// setPriorityByID updates the priority of a notification using only its ID.
func (m *Model) setPriorityByID(id string, priority int) tea.Cmd {
	return m.submitMutationTask("priority:"+id, func(ctx context.Context) (mutationAppliedMsg, error) {
		result, err := m.backend.SetPriority(ctx, id, priority)
		if err != nil {
			return mutationAppliedMsg{}, err
		}
		return mutationAppliedMsg{notifications: result.Notifications, toast: result.Toast, err: result.Err}, nil
	})
}

func (m *Model) submitMutationTask(scope string, run func(context.Context) (mutationAppliedMsg, error)) tea.Cmd {
	return m.submitTask(scope, 0, api.PriorityUser, func(ctx context.Context) any {
		msg, err := run(ctx)
		if err != nil {
			return types.ErrMsg{Err: err}
		}
		return msg
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
			res, err := m.backend.FetchDetail(ctx, url, string(st), force)
			if err != nil {
				return types.ErrMsg{Err: err}
			}
			if err := m.backend.PersistFetchedDetail(ctx, id, url, res); err != nil {
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
			results := m.backend.FetchHybridBatch(ctx, chunk, force)
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
		res, err := m.backend.FetchDetail(ctx, u, string(subjectType), force)
		if err != nil {
			return types.ErrMsg{Err: err}
		}

		if err := m.backend.PersistFetchedDetail(ctx, id, u, res); err != nil {
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
