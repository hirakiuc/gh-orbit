package tui

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/api"
	"github.com/hirakiuc/gh-orbit/internal/triage"
	"github.com/hirakiuc/gh-orbit/internal/types"
)

func (m *Model) ApplyNotificationBatch(request types.NotificationBatchRequest) tea.Cmd {
	normalized, err := types.NormalizeNotificationBatchRequest(request)
	before := append([]triage.NotificationWithState(nil), m.allNotifications...)
	if err != nil {
		return func() tea.Msg {
			return batchMutationAppliedMsg{before: before, result: types.NotificationBatchResult{
				Status: types.NotificationBatchRejected, Request: request, Notifications: before, Err: err,
			}}
		}
	}

	m.pendingBatchRequest = normalized
	m.batchPending = true
	m.allNotifications = applyBatchOptimistically(m.allNotifications, normalized)
	m.applyFilters()

	return m.submitBackendTask("notification-batch", 0, func(ctx context.Context) any {
		result, callErr := m.backend.ApplyNotificationBatch(ctx, normalized)
		if callErr != nil {
			result = types.NotificationBatchResult{
				Status: types.NotificationBatchCommitUnknown, Reconciliation: types.NotificationBatchReconciliationPending,
				Request: normalized, Notifications: before, Err: callErr,
			}
		}
		return batchMutationAppliedMsg{result: result, before: before}
	})
}

func applyBatchOptimistically(notifications []triage.NotificationWithState, request types.NotificationBatchRequest) []triage.NotificationWithState {
	cloned := append([]triage.NotificationWithState(nil), notifications...)
	selected := make(map[string]struct{}, len(request.IDs))
	for _, id := range request.IDs {
		selected[id] = struct{}{}
	}
	for index := range cloned {
		if _, ok := selected[cloned[index].GitHubID]; !ok {
			continue
		}
		switch request.Operation {
		case types.NotificationBatchRead:
			cloned[index].IsReadLocally = true
		case types.NotificationBatchUnread:
			cloned[index].IsReadLocally = false
		case types.NotificationBatchHandled:
			cloned[index].IsHandledLocally = true
		case types.NotificationBatchUnhandled:
			cloned[index].IsHandledLocally = false
		}
	}
	return cloned
}

func (m *Model) selectedBatchRequest(operation types.NotificationBatchOperation) (types.NotificationBatchRequest, bool) {
	if len(m.selectedIDs) == 0 {
		return types.NotificationBatchRequest{}, false
	}
	ids := make([]string, 0, len(m.selectedIDs))
	for id := range m.selectedIDs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return types.NotificationBatchRequest{Operation: operation, IDs: ids}, true
}

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
			break
		}
	}

	m.applyFilters()

	// 2. Persistent Local & Remote Update via Traffic Controller
	return m.submitMutationTask("read:"+id, func(ctx context.Context) (mutationAppliedMsg, error) {
		result, err := m.backend.SetRead(ctx, id, read)
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

// SetHandledByID changes only the local triage state and immediately reconciles
// list/detail identity if the active filter removes the target.
func (m *Model) SetHandledByID(id string, handled bool, previousIndex int) tea.Cmd {
	before := append([]triage.NotificationWithState(nil), m.allNotifications...)
	for idx, n := range m.allNotifications {
		if n.GitHubID == id {
			m.allNotifications[idx].IsHandledLocally = handled
			break
		}
	}

	m.applyFilters()
	m.reconcileHandledTarget(id, previousIndex, true)

	return m.submitMutationTask("handled:"+id, func(ctx context.Context) (mutationAppliedMsg, error) {
		result, err := m.backend.SetHandled(ctx, id, handled)
		if err != nil {
			notifications, reloadErr := m.backend.ListNotifications(ctx)
			if reloadErr != nil {
				notifications = before
			}
			return mutationAppliedMsg{
				notifications: notifications,
				toast:         "Failed to update handled state",
				err:           err,
				targetID:      id,
				previousIndex: previousIndex,
				reconcileItem: true,
			}, nil
		}
		return mutationAppliedMsg{
			notifications: result.Notifications,
			toast:         result.Toast,
			err:           result.Err,
			targetID:      id,
			previousIndex: previousIndex,
			reconcileItem: true,
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
