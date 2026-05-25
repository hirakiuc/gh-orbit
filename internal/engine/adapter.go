package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/api"
	"github.com/hirakiuc/gh-orbit/internal/models"
	"github.com/hirakiuc/gh-orbit/internal/triage"
	"github.com/hirakiuc/gh-orbit/internal/types"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

type syncToolStatus string

const (
	syncToolStatusOK                 syncToolStatus = "ok"
	syncToolStatusIntervalNotReached syncToolStatus = "interval_not_reached"
)

type syncToolResult struct {
	Status    syncToolStatus       `json:"status"`
	RateLimit models.RateLimitInfo `json:"rate_limit,omitempty"`
}

// MCPAdapter implements core interfaces by proxying to an MCP server.
type MCPAdapter struct {
	client client.MCPClient

	onMutation func()
	mu         sync.RWMutex

	debounceTimer *time.Timer
	debounceMu    sync.Mutex
	shutdown      atomic.Bool
}

func NewMCPAdapter(c client.MCPClient) *MCPAdapter {
	a := &MCPAdapter{
		client: c,
	}

	if c != nil {
		c.OnNotification(func(notification mcp.JSONRPCNotification) {
			if notification.Method == mcp.MethodNotificationResourcesListChanged {
				a.handleResourceUpdate(notification)
			}
		})
	}

	return a
}

func (a *MCPAdapter) OnMutation(fn func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.onMutation = fn
}

func (a *MCPAdapter) handleResourceUpdate(n mcp.JSONRPCNotification) {
	if a.shutdown.Load() {
		return
	}

	a.debounceMu.Lock()
	defer a.debounceMu.Unlock()

	if a.shutdown.Load() {
		return
	}

	if a.debounceTimer != nil {
		a.debounceTimer.Stop()
	}

	a.debounceTimer = time.AfterFunc(200*time.Millisecond, func() {
		if a.shutdown.Load() {
			return
		}
		a.mu.RLock()
		if a.onMutation != nil {
			a.onMutation()
		}
		a.mu.RUnlock()
	})
}

// --- types.TUIBackend / types.NotificationStore Implementation ---

func (a *MCPAdapter) ListNotifications(ctx context.Context) ([]triage.NotificationWithState, error) {
	resp, err := a.client.ReadResource(ctx, mcp.ReadResourceRequest{
		Params: mcp.ReadResourceParams{
			URI: "gh-orbit://notifications/all",
		},
	})
	if err != nil {
		return nil, err
	}
	if resp == nil || len(resp.Contents) == 0 {
		return nil, nil
	}

	var notifs []triage.NotificationWithState
	// mcp-go ResourceContents has Text field in TextResourceContents
	// We need to check the type or just access if we are sure
	content, ok := resp.Contents[0].(mcp.TextResourceContents)
	if !ok {
		// Try to marshal if it's an object, or just fail
		return nil, fmt.Errorf("unexpected resource content type")
	}

	err = json.Unmarshal([]byte(content.Text), &notifs)
	return notifs, err
}

func (a *MCPAdapter) ResolveUserID(ctx context.Context) (string, error) {
	resp, err := a.client.ReadResource(ctx, mcp.ReadResourceRequest{
		Params: mcp.ReadResourceParams{
			URI: "gh-orbit://session/user",
		},
	})
	if err != nil {
		return "", err
	}
	if resp == nil || len(resp.Contents) == 0 {
		return "", errors.New("current user resource returned no content")
	}

	content, ok := resp.Contents[0].(mcp.TextResourceContents)
	if !ok {
		return "", fmt.Errorf("unexpected current user resource content type")
	}

	var payload struct {
		Login string `json:"login"`
	}
	if err := json.Unmarshal([]byte(content.Text), &payload); err != nil {
		return "", err
	}
	if payload.Login == "" {
		return "", errors.New("current user login is empty")
	}
	return payload.Login, nil
}

func (a *MCPAdapter) MarkRead(ctx context.Context, id string, isRead bool) (types.MarkReadResult, error) {
	before, _ := a.ListNotifications(ctx)

	resp, err := a.client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "mark_read",
			Arguments: map[string]any{
				"id":   id,
				"read": isRead,
			},
		},
	})
	if err != nil {
		return types.MarkReadResult{}, err
	}
	if resp == nil {
		return types.MarkReadResult{}, errors.New("mark_read returned nil result")
	}
	if resp.IsError {
		toolErr := decodeToolResultError("mark_read", resp)
		if api.IsRemoteMarkReadFailure(toolErr) {
			notifications, reloadErr := a.ListNotifications(ctx)
			if reloadErr != nil {
				notifications = applyReadState(before, id, isRead)
			}
			return types.MarkReadResult{
				Status:        types.MarkReadRemoteFailure,
				Notifications: notifications,
				Toast:         "Marked read locally; GitHub sync failed",
				Err:           toolErr,
			}, nil
		}
		notifications, reloadErr := a.ListNotifications(ctx)
		if reloadErr != nil {
			if before != nil {
				return types.MarkReadResult{
					Status:        types.MarkReadLocalFailure,
					Notifications: before,
					Toast:         "Failed to update read state",
					Err:           toolErr,
				}, nil
			}
			return types.MarkReadResult{}, fmt.Errorf("reload notifications after local read failure: %w (original error: %v)", reloadErr, toolErr)
		}
		return types.MarkReadResult{
			Status:        types.MarkReadLocalFailure,
			Notifications: notifications,
			Toast:         "Failed to update read state",
			Err:           toolErr,
		}, nil
	}
	notifications, err := a.ListNotifications(ctx)
	if err != nil {
		if before != nil {
			notifications = applyReadState(before, id, isRead)
		} else {
			return types.MarkReadResult{}, err
		}
	}
	return types.MarkReadResult{
		Status:        types.MarkReadSuccess,
		Notifications: notifications,
	}, nil
}

func (a *MCPAdapter) MarkReadLocally(ctx context.Context, id string, isRead bool) error {
	_, err := a.MarkRead(ctx, id, isRead)
	return err
}

func (a *MCPAdapter) SetPriority(ctx context.Context, id string, priority int) (types.PriorityUpdateResult, error) {
	before, _ := a.ListNotifications(ctx)

	resp, err := a.client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "set_priority",
			Arguments: map[string]any{
				"id":    id,
				"level": float64(priority),
			},
		},
	})
	if err != nil {
		return types.PriorityUpdateResult{}, err
	}
	if resp != nil && resp.IsError {
		toolErr := decodeToolResultError("set_priority", resp)
		notifications, reloadErr := a.ListNotifications(ctx)
		if reloadErr != nil {
			if before != nil {
				return types.PriorityUpdateResult{
					Status:        types.PriorityUpdateFailure,
					Notifications: before,
					Err:           toolErr,
				}, nil
			}
			return types.PriorityUpdateResult{}, fmt.Errorf("reload notifications after priority update failure: %w (original error: %v)", reloadErr, toolErr)
		}
		return types.PriorityUpdateResult{
			Status:        types.PriorityUpdateFailure,
			Notifications: notifications,
			Err:           toolErr,
		}, nil
	}
	notifications, err := a.ListNotifications(ctx)
	if err != nil {
		if before != nil {
			notifications = applyPriority(before, id, priority)
		} else {
			return types.PriorityUpdateResult{}, err
		}
	}
	return types.PriorityUpdateResult{
		Status:        types.PriorityUpdateSuccess,
		Notifications: notifications,
		Toast:         priorityToast(priority),
	}, nil
}

func (a *MCPAdapter) PersistFetchedDetail(ctx context.Context, id, sourceURL string, res models.EnrichmentResult) error {
	_, err := a.client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "persist_fetched_detail",
			Arguments: map[string]any{
				"id":                 id,
				"source_url":         sourceURL,
				"node_id":            res.SubjectNodeID,
				"body":               res.Body,
				"author":             res.Author,
				"html_url":           res.HTMLURL,
				"resource_state":     res.ResourceState,
				"resource_sub_state": res.ResourceSubState,
			},
		},
	})
	return err
}

func (a *MCPAdapter) PersistIndependentDetail(ctx context.Context, id, nodeID, body, author, htmlURL, resourceState, resourceSubState string) error {
	_, err := a.client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "enrich_notification",
			Arguments: map[string]any{
				"id":                 id,
				"node_id":            nodeID,
				"body":               body,
				"author":             author,
				"html_url":           htmlURL,
				"resource_state":     resourceState,
				"resource_sub_state": resourceSubState,
			},
		},
	})
	return err
}

func (a *MCPAdapter) EnrichNotification(ctx context.Context, id, nodeID, body, author, htmlURL, resourceState, resourceSubState string) error {
	return a.PersistIndependentDetail(ctx, id, nodeID, body, author, htmlURL, resourceState, resourceSubState)
}

// --- types.TUIBackend Implementation ---

func (a *MCPAdapter) Sync(ctx context.Context, force bool) (models.RateLimitInfo, error) {
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, types.ConnectedSyncTimeout)
		defer cancel()
	}

	resp, err := a.client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "sync",
			Arguments: map[string]any{
				"force": force,
			},
		},
	})
	if err != nil {
		return models.RateLimitInfo{}, err
	}

	if payload, ok := decodeSyncToolResult(resp); ok {
		switch payload.Status {
		case syncToolStatusOK:
			return payload.RateLimit, nil
		case syncToolStatusIntervalNotReached:
			return payload.RateLimit, types.ErrSyncIntervalNotReached
		default:
			return models.RateLimitInfo{}, fmt.Errorf("sync error: invalid sync tool status %q", payload.Status)
		}
	}

	if resp.IsError {
		content, _ := resp.Content[0].(mcp.TextContent)
		return models.RateLimitInfo{}, fmt.Errorf("sync error: %s", content.Text)
	}

	var rl models.RateLimitInfo
	if len(resp.Content) > 0 {
		if text, ok := resp.Content[0].(mcp.TextContent); ok {
			_ = json.Unmarshal([]byte(text.Text), &rl)
		}
	}

	return rl, nil
}

func decodeSyncToolResult(resp *mcp.CallToolResult) (syncToolResult, bool) {
	if resp == nil || resp.StructuredContent == nil {
		return syncToolResult{}, false
	}

	raw, err := json.Marshal(resp.StructuredContent)
	if err != nil {
		return syncToolResult{}, false
	}

	var payload syncToolResult
	if err := json.Unmarshal(raw, &payload); err != nil {
		return syncToolResult{}, false
	}
	if payload.Status == "" {
		return syncToolResult{}, false
	}

	return payload, true
}

func decodeToolResultError(toolName string, resp *mcp.CallToolResult) error {
	if resp == nil {
		return fmt.Errorf("%s error: empty response", toolName)
	}
	if len(resp.Content) == 0 {
		return fmt.Errorf("%s error: unknown tool failure", toolName)
	}
	if text, ok := resp.Content[0].(mcp.TextContent); ok && text.Text != "" {
		return errors.New(text.Text)
	}
	return fmt.Errorf("%s error: unexpected tool failure payload", toolName)
}

func applyReadState(notifications []triage.NotificationWithState, id string, read bool) []triage.NotificationWithState {
	cloned := append([]triage.NotificationWithState(nil), notifications...)
	for idx := range cloned {
		if cloned[idx].GitHubID == id {
			cloned[idx].IsReadLocally = read
			break
		}
	}
	return cloned
}

func applyPriority(notifications []triage.NotificationWithState, id string, priority int) []triage.NotificationWithState {
	cloned := append([]triage.NotificationWithState(nil), notifications...)
	for idx := range cloned {
		if cloned[idx].GitHubID == id {
			cloned[idx].Priority = priority
			break
		}
	}
	return cloned
}

func priorityToast(priority int) string {
	switch priority {
	case 1:
		return "Priority set to Low"
	case 2:
		return "Priority set to Medium"
	case 3:
		return "Priority set to High"
	default:
		return "Priority cleared"
	}
}

func (a *MCPAdapter) Shutdown(ctx context.Context) {
	a.shutdown.Store(true)

	a.debounceMu.Lock()
	defer a.debounceMu.Unlock()

	if a.debounceTimer != nil {
		a.debounceTimer.Stop()
		a.debounceTimer = nil
	}
}
func (a *MCPAdapter) BridgeStatus() types.BridgeStatus { return types.StatusHealthy }

// --- types.Enricher Implementation ---

func (a *MCPAdapter) FetchDetail(ctx context.Context, u string, subjectType string, force bool) (models.EnrichmentResult, error) {
	resp, err := a.client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "fetch_detail",
			Arguments: map[string]any{
				"url":          u,
				"subject_type": subjectType,
				"force":        force,
			},
		},
	})
	if err != nil {
		return models.EnrichmentResult{}, err
	}

	if resp.IsError {
		content, _ := resp.Content[0].(mcp.TextContent)
		return models.EnrichmentResult{}, fmt.Errorf("fetch detail error: %s", content.Text)
	}

	var result models.EnrichmentResult
	if len(resp.Content) > 0 {
		if text, ok := resp.Content[0].(mcp.TextContent); ok {
			if err := json.Unmarshal([]byte(text.Text), &result); err != nil {
				return models.EnrichmentResult{}, err
			}
		}
	}

	return result, nil
}

func (a *MCPAdapter) FetchHybridBatch(ctx context.Context, notifications []triage.NotificationWithState, force bool) map[string]models.EnrichmentResult {
	resp, err := a.client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "fetch_hybrid_batch",
			Arguments: map[string]any{
				"notifications": notifications,
				"force":         force,
			},
		},
	})
	if err != nil || resp == nil || resp.IsError {
		return nil
	}

	var results map[string]models.EnrichmentResult
	if len(resp.Content) > 0 {
		if text, ok := resp.Content[0].(mcp.TextContent); ok {
			if err := json.Unmarshal([]byte(text.Text), &results); err != nil {
				return nil
			}
		}
	}

	return results
}

// Ensure MCPAdapter implements required interfaces
var (
	_ types.TUIBackend = (*MCPAdapter)(nil)
	_ types.Enricher   = (*MCPAdapter)(nil)
)
