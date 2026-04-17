package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/models"
	"github.com/hirakiuc/gh-orbit/internal/triage"
	"github.com/hirakiuc/gh-orbit/internal/types"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

// MCPAdapter implements core interfaces by proxying to an MCP server.
type MCPAdapter struct {
	client client.MCPClient

	onMutation func()
	mu         sync.RWMutex

	debounceTimer *time.Timer
	debounceMu    sync.Mutex
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
	a.debounceMu.Lock()
	defer a.debounceMu.Unlock()

	if a.debounceTimer != nil {
		a.debounceTimer.Stop()
	}

	a.debounceTimer = time.AfterFunc(200*time.Millisecond, func() {
		a.mu.RLock()
		if a.onMutation != nil {
			a.onMutation()
		}
		a.mu.RUnlock()
	})
}

// --- types.Repository Implementation ---

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

// No-ops for client mode (Engine is authority)
func (a *MCPAdapter) EnrichNotification(ctx context.Context, id, nodeID, body, author, htmlURL, resourceState, resourceSubState string) error {
	return nil
}

func (a *MCPAdapter) UpdateResourceStateByNodeID(ctx context.Context, nodeID, state, resourceSubState string) error {
	return nil
}
func (a *MCPAdapter) UpdateSyncMeta(ctx context.Context, s models.SyncMeta) error { return nil }
func (a *MCPAdapter) MarkNotifiedBatch(ctx context.Context, ids []string) error   { return nil }
func (a *MCPAdapter) UpdateBridgeHealth(ctx context.Context, h models.BridgeHealth) error {
	return nil
}

// Stubs for remaining Repository methods
func (a *MCPAdapter) GetNotification(ctx context.Context, id string) (*triage.NotificationWithState, error) {
	return nil, nil
}

func (a *MCPAdapter) GetSyncMeta(ctx context.Context, userID, key string) (*models.SyncMeta, error) {
	return nil, nil
}

func (a *MCPAdapter) GetBridgeHealth(ctx context.Context) (*models.BridgeHealth, error) {
	return nil, nil
}

func (a *MCPAdapter) UpsertNotifications(ctx context.Context, notifications []triage.Notification) error {
	return nil
}
func (a *MCPAdapter) ArchiveThread(ctx context.Context, id string) error   { return nil }
func (a *MCPAdapter) UnarchiveThread(ctx context.Context, id string) error { return nil }
func (a *MCPAdapter) MuteThread(ctx context.Context, id string) error      { return nil }
func (a *MCPAdapter) UnmuteThread(ctx context.Context, id string) error    { return nil }

// --- types.Syncer Implementation ---

func (a *MCPAdapter) Sync(ctx context.Context, userID string, force bool) (models.RateLimitInfo, error) {
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

func (a *MCPAdapter) Shutdown(ctx context.Context)     {}
func (a *MCPAdapter) BridgeStatus() types.BridgeStatus { return types.StatusHealthy }

// --- types.Enricher Implementation ---

func (a *MCPAdapter) FetchDetail(ctx context.Context, u string, subjectType string, force bool) (models.EnrichmentResult, error) {
	return models.EnrichmentResult{}, nil
}

func (a *MCPAdapter) FetchHybridBatch(ctx context.Context, notifications []triage.NotificationWithState, force bool) map[string]models.EnrichmentResult {
	return nil
}

// Ensure MCPAdapter implements required interfaces
var (
	_ types.Repository = (*MCPAdapter)(nil)
	_ types.Syncer     = (*MCPAdapter)(nil)
	_ types.Enricher   = (*MCPAdapter)(nil)
)
