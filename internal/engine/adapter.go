package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/api"
	"github.com/hirakiuc/gh-orbit/internal/github"
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

// --- types.NotificationStore Implementation ---

func (a *MCPAdapter) ListNotifications(ctx context.Context) ([]triage.NotificationWithState, error) {
	resp, err := a.client.ReadResource(ctx, mcp.ReadResourceRequest{
		Params: mcp.ReadResourceParams{
			URI: "gh-orbit://notifications/all",
		},
	})
	if err != nil {
		return nil, err
	}

	if len(resp.Contents) == 0 {
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

func (a *MCPAdapter) MarkReadLocally(ctx context.Context, id string, isRead bool) error {
	_, err := a.client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "mark_read",
			Arguments: map[string]any{
				"id":   id,
				"read": isRead,
			},
		},
	})
	return err
}

func (a *MCPAdapter) SetPriority(ctx context.Context, id string, priority int) error {
	_, err := a.client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "set_priority",
			Arguments: map[string]any{
				"id":    id,
				"level": float64(priority),
			},
		},
	})
	return err
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

// --- types.Syncer Implementation ---

func (a *MCPAdapter) Sync(ctx context.Context, userID string, force bool) (models.RateLimitInfo, error) {
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

// --- api.Alerter Implementation ---

func (a *MCPAdapter) Notify(ctx context.Context, n github.Notification) error { return nil }
func (a *MCPAdapter) SyncStart(ctx context.Context)                           {}
func (a *MCPAdapter) ActiveTierInfo() (string, types.BridgeStatus) {
	return "Connected", types.StatusHealthy
}

func (a *MCPAdapter) TestNotify(ctx context.Context, title, subtitle, body string) error {
	return nil
}

// Ensure MCPAdapter implements required interfaces
var (
	_ types.NotificationStore = (*MCPAdapter)(nil)
	_ types.Syncer            = (*MCPAdapter)(nil)
	_ types.Enricher          = (*MCPAdapter)(nil)
	_ api.Alerter             = (*MCPAdapter)(nil)
)
