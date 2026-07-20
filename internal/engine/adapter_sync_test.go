package engine

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/api"
	"github.com/hirakiuc/gh-orbit/internal/models"
	"github.com/hirakiuc/gh-orbit/internal/triage"
	"github.com/hirakiuc/gh-orbit/internal/types"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func notificationResource(t *testing.T, request mcp.ReadResourceRequest, notifications []triage.NotificationWithState) *mcp.ReadResourceResult {
	t.Helper()
	payload, err := json.Marshal(notifications)
	require.NoError(t, err)
	return &mcp.ReadResourceResult{Contents: []mcp.ResourceContents{
		mcp.TextResourceContents{URI: request.Params.URI, Text: string(payload)},
	}}
}

func TestMCPAdapter_NotificationBatchTransportLossIsCommitUnknown(t *testing.T) {
	callCount := 0
	adapter := NewMCPAdapter(&blockingMCPClient{
		readResource: func(_ context.Context, request mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return notificationResource(t, request, []triage.NotificationWithState{{Notification: triage.Notification{GitHubID: "1"}}}), nil
		},
		callTool: func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			callCount++
			return nil, errors.New("connection lost")
		},
	})

	result, err := adapter.ApplyNotificationBatch(context.Background(), types.NotificationBatchRequest{
		Operation: types.NotificationBatchRead, IDs: []string{"1"},
	})
	require.NoError(t, err)
	assert.Equal(t, types.NotificationBatchCommitUnknown, result.Status)
	assert.Equal(t, types.NotificationBatchReconciliationPending, result.Reconciliation)
	assert.Equal(t, 1, callCount, "an indeterminate request must not be replayed")
}

func TestMCPAdapter_NotificationBatchConfirmedCommitSurvivesReloadFailure(t *testing.T) {
	reads := 0
	outcomes := []types.NotificationBatchItemResult{{ID: "1", Status: types.NotificationRemoteFailed, ErrorCode: "remote_failed"}}
	adapter := NewMCPAdapter(&blockingMCPClient{
		readResource: func(_ context.Context, request mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			reads++
			if reads > 1 {
				return nil, errors.New("reload failed")
			}
			return notificationResource(t, request, []triage.NotificationWithState{{Notification: triage.Notification{GitHubID: "1"}}}), nil
		},
		callTool: func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{StructuredContent: map[string]any{
				"status": "committed", "reconciliation": "authoritative",
				"request":  map[string]any{"operation": "read", "ids": []string{"1"}},
				"outcomes": outcomes,
			}}, nil
		},
	})

	result, err := adapter.ApplyNotificationBatch(context.Background(), types.NotificationBatchRequest{
		Operation: types.NotificationBatchRead, IDs: []string{"1"},
	})
	require.NoError(t, err)
	assert.Equal(t, types.NotificationBatchCommitted, result.Status)
	assert.Equal(t, types.NotificationBatchReconciliationPending, result.Reconciliation)
	assert.Equal(t, outcomes, result.Outcomes)
}

func TestMCPAdapter_NotificationBatchRejectsUntrustworthyStructuredResults(t *testing.T) {
	validRequest := map[string]any{"operation": "read", "ids": []string{"1"}}
	tests := map[string]map[string]any{
		"malformed": {"outcomes": make(chan int)},
		"missing request": {
			"status": "committed", "reconciliation": "authoritative",
			"outcomes": []map[string]any{{"id": "1", "status": "succeeded"}},
		},
		"missing outcomes": {
			"status": "committed", "reconciliation": "authoritative", "request": validRequest,
		},
		"duplicate target": {
			"status": "committed", "reconciliation": "authoritative", "request": validRequest,
			"outcomes": []map[string]any{{"id": "1", "status": "succeeded"}, {"id": "1", "status": "succeeded"}},
		},
		"foreign target": {
			"status": "committed", "reconciliation": "authoritative", "request": validRequest,
			"outcomes": []map[string]any{{"id": "2", "status": "succeeded"}},
		},
		"invalid status": {
			"status": "committed", "reconciliation": "authoritative", "request": validRequest,
			"outcomes": []map[string]any{{"id": "1", "status": "running"}},
		},
		"unbounded error code": {
			"status": "committed", "reconciliation": "authoritative", "request": validRequest,
			"outcomes": []map[string]any{{"id": "1", "status": "failed", "error_code": string(make([]byte, 65))}},
		},
		"contradictory local-only result": {
			"status": "committed", "reconciliation": "authoritative",
			"request":  map[string]any{"operation": "handled", "ids": []string{"1"}},
			"outcomes": []map[string]any{{"id": "1", "status": "succeeded"}},
		},
		"mismatched operation": {
			"status": "committed", "reconciliation": "authoritative",
			"request":  map[string]any{"operation": "unread", "ids": []string{"1"}},
			"outcomes": []map[string]any{{"id": "1", "status": "not_required"}},
		},
	}

	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			calls := 0
			adapter := NewMCPAdapter(&blockingMCPClient{
				readResource: func(_ context.Context, request mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
					return notificationResource(t, request, []triage.NotificationWithState{{Notification: triage.Notification{GitHubID: "1"}}}), nil
				},
				callTool: func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					calls++
					return &mcp.CallToolResult{StructuredContent: payload}, nil
				},
			})
			operation := types.NotificationBatchRead
			if name == "contradictory local-only result" {
				operation = types.NotificationBatchHandled
			}
			result, err := adapter.ApplyNotificationBatch(context.Background(), types.NotificationBatchRequest{Operation: operation, IDs: []string{"1"}})
			require.NoError(t, err)
			assert.Equal(t, types.NotificationBatchCommitUnknown, result.Status)
			assert.Equal(t, 1, calls, "an untrustworthy response must not be replayed")
		})
	}
}

type blockingMCPClient struct {
	callTool     func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error)
	readResource func(ctx context.Context, request mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error)
}

var _ client.MCPClient = (*blockingMCPClient)(nil)

func (c *blockingMCPClient) Initialize(ctx context.Context, request mcp.InitializeRequest) (*mcp.InitializeResult, error) {
	return nil, nil
}

func (c *blockingMCPClient) Ping(ctx context.Context) error { return nil }

func (c *blockingMCPClient) ListResourcesByPage(ctx context.Context, request mcp.ListResourcesRequest) (*mcp.ListResourcesResult, error) {
	return nil, nil
}

func (c *blockingMCPClient) ListResources(ctx context.Context, request mcp.ListResourcesRequest) (*mcp.ListResourcesResult, error) {
	return nil, nil
}

func (c *blockingMCPClient) ListResourceTemplatesByPage(ctx context.Context, request mcp.ListResourceTemplatesRequest) (*mcp.ListResourceTemplatesResult, error) {
	return nil, nil
}

func (c *blockingMCPClient) ListResourceTemplates(ctx context.Context, request mcp.ListResourceTemplatesRequest) (*mcp.ListResourceTemplatesResult, error) {
	return nil, nil
}

func (c *blockingMCPClient) ReadResource(ctx context.Context, request mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	if c.readResource != nil {
		return c.readResource(ctx, request)
	}
	return nil, nil
}

func (c *blockingMCPClient) Subscribe(ctx context.Context, request mcp.SubscribeRequest) error {
	return nil
}

func (c *blockingMCPClient) Unsubscribe(ctx context.Context, request mcp.UnsubscribeRequest) error {
	return nil
}

func (c *blockingMCPClient) ListPromptsByPage(ctx context.Context, request mcp.ListPromptsRequest) (*mcp.ListPromptsResult, error) {
	return nil, nil
}

func (c *blockingMCPClient) ListPrompts(ctx context.Context, request mcp.ListPromptsRequest) (*mcp.ListPromptsResult, error) {
	return nil, nil
}

func (c *blockingMCPClient) GetPrompt(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	return nil, nil
}

func (c *blockingMCPClient) ListToolsByPage(ctx context.Context, request mcp.ListToolsRequest) (*mcp.ListToolsResult, error) {
	return nil, nil
}

func (c *blockingMCPClient) ListTools(ctx context.Context, request mcp.ListToolsRequest) (*mcp.ListToolsResult, error) {
	return nil, nil
}

func (c *blockingMCPClient) CallTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if c.callTool != nil {
		return c.callTool(ctx, request)
	}
	return nil, nil
}

func (c *blockingMCPClient) SetLevel(ctx context.Context, request mcp.SetLevelRequest) error {
	return nil
}

func (c *blockingMCPClient) Complete(ctx context.Context, request mcp.CompleteRequest) (*mcp.CompleteResult, error) {
	return nil, nil
}

func (c *blockingMCPClient) Close() error { return nil }

func (c *blockingMCPClient) OnNotification(handler func(notification mcp.JSONRPCNotification)) {}

func TestMCPAdapter_SyncAddsFallbackTimeoutWhenCallerHasNoDeadline(t *testing.T) {
	originalTimeout := types.ConnectedSyncTimeout
	types.ConnectedSyncTimeout = 10 * time.Millisecond
	t.Cleanup(func() {
		types.ConnectedSyncTimeout = originalTimeout
	})

	var sawDeadline bool
	adapter := NewMCPAdapter(&blockingMCPClient{
		callTool: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			_, sawDeadline = ctx.Deadline()
			<-ctx.Done()
			return nil, ctx.Err()
		},
	})

	_, err := adapter.Sync(context.Background(), true)
	require.Error(t, err)
	assert.True(t, sawDeadline)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestMCPAdapter_SyncRespectsCallerDeadline(t *testing.T) {
	originalTimeout := types.ConnectedSyncTimeout
	types.ConnectedSyncTimeout = 200 * time.Millisecond
	t.Cleanup(func() {
		types.ConnectedSyncTimeout = originalTimeout
	})

	parentCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	parentDeadline, ok := parentCtx.Deadline()
	require.True(t, ok)

	var seenDeadline time.Time
	adapter := NewMCPAdapter(&blockingMCPClient{
		callTool: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var hasDeadline bool
			seenDeadline, hasDeadline = ctx.Deadline()
			require.True(t, hasDeadline)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	})

	_, err := adapter.Sync(parentCtx, true)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, parentDeadline, seenDeadline)
}

func TestMCPAdapter_SyncPassesThroughSuccessfulResponse(t *testing.T) {
	adapter := NewMCPAdapter(&blockingMCPClient{
		callTool: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				StructuredContent: syncToolResult{
					Status:    syncToolStatusOK,
					RateLimit: models.RateLimitInfo{Remaining: 123},
				},
				Content: []mcp.Content{
					mcp.NewTextContent(`{"remaining":123}`),
				},
			}, nil
		},
	})

	rl, err := adapter.Sync(context.Background(), true)
	require.NoError(t, err)
	assert.Equal(t, models.RateLimitInfo{Remaining: 123}, rl)
}

func TestMCPAdapter_SyncReconstructsIntervalNotReachedFromStructuredResult(t *testing.T) {
	adapter := NewMCPAdapter(&blockingMCPClient{
		callTool: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				StructuredContent: syncToolResult{
					Status:    syncToolStatusIntervalNotReached,
					RateLimit: models.RateLimitInfo{Remaining: 456},
				},
				Content: []mcp.Content{
					mcp.NewTextContent(`sync interval not reached`),
				},
			}, nil
		},
	})

	rl, err := adapter.Sync(context.Background(), false)
	assert.Equal(t, models.RateLimitInfo{Remaining: 456}, rl)
	assert.ErrorIs(t, err, types.ErrSyncIntervalNotReached)
}

func TestMCPAdapter_SyncFallsBackToLegacySuccessParsingWithoutStructuredContent(t *testing.T) {
	adapter := NewMCPAdapter(&blockingMCPClient{
		callTool: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(`{"remaining":789}`),
				},
			}, nil
		},
	})

	rl, err := adapter.Sync(context.Background(), true)
	require.NoError(t, err)
	assert.Equal(t, models.RateLimitInfo{Remaining: 789}, rl)
}

func TestMCPAdapter_SyncFallsBackToLegacyErrorWithoutStructuredContent(t *testing.T) {
	adapter := NewMCPAdapter(&blockingMCPClient{
		callTool: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{
					mcp.NewTextContent(`sync failed: boom`),
				},
			}, nil
		},
	})

	_, err := adapter.Sync(context.Background(), true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sync error: sync failed: boom")
}

func TestMCPAdapter_SyncRejectsInvalidStructuredContract(t *testing.T) {
	adapter := NewMCPAdapter(&blockingMCPClient{
		callTool: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				StructuredContent: map[string]any{
					"status": "unknown",
				},
				Content: []mcp.Content{
					mcp.NewTextContent(`{"remaining":123}`),
				},
			}, nil
		},
	})

	_, err := adapter.Sync(context.Background(), true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `invalid sync tool status "unknown"`)
}

func TestMCPAdapter_MarkRead_ClassifiesRemoteFailureFromToolResult(t *testing.T) {
	adapter := NewMCPAdapter(&blockingMCPClient{
		callTool: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			require.Equal(t, "set_read", request.Params.Name)
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{
					mcp.NewTextContent("failed to mark read on GitHub: boom"),
				},
			}, nil
		},
	})

	result, err := adapter.SetRead(context.Background(), "123", true)
	require.NoError(t, err)
	assert.Equal(t, types.MarkReadRemoteFailure, result.Status)
	require.Error(t, result.Err)
	assert.Contains(t, result.Err.Error(), "failed to mark read on GitHub: boom")
}

func TestMCPAdapter_MarkRead_TreatsLocalToolFailureAsError(t *testing.T) {
	adapter := NewMCPAdapter(&blockingMCPClient{
		callTool: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			require.Equal(t, "set_read", request.Params.Name)
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{
					mcp.NewTextContent("failed to mark read locally: sqlite busy"),
				},
			}, nil
		},
	})

	result, err := adapter.SetRead(context.Background(), "123", true)
	require.NoError(t, err)
	assert.Equal(t, types.MarkReadLocalFailure, result.Status)
	require.Error(t, result.Err)
	assert.Contains(t, result.Err.Error(), "failed to mark read locally: sqlite busy")
}

func TestMCPAdapter_ResolveUserID_ReadsEffectiveSessionIdentity(t *testing.T) {
	adapter := NewMCPAdapter(&blockingMCPClient{
		readResource: func(ctx context.Context, request mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			require.Equal(t, "gh-orbit://session/user", request.Params.URI)
			return &mcp.ReadResourceResult{
				Contents: []mcp.ResourceContents{
					mcp.TextResourceContents{
						URI:      "gh-orbit://session/user",
						MIMEType: "application/json",
						Text:     `{"login":"hirakiuc"}`,
					},
				},
			}, nil
		},
	})

	userID, err := adapter.ResolveUserID(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "hirakiuc", userID)
}

func TestMCPAdapter_ResolveUserID_RejectsEmptyIdentity(t *testing.T) {
	adapter := NewMCPAdapter(&blockingMCPClient{
		readResource: func(ctx context.Context, request mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return &mcp.ReadResourceResult{
				Contents: []mcp.ResourceContents{
					mcp.TextResourceContents{
						URI:      "gh-orbit://session/user",
						MIMEType: "application/json",
						Text:     `{"login":""}`,
					},
				},
			}, nil
		},
	})

	_, err := adapter.ResolveUserID(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "current user login is empty")
}

func TestMCPAdapter_ImplementsTUIBackendBoundary(t *testing.T) {
	var backend types.TUIBackend = NewMCPAdapter(nil)
	require.NotNil(t, backend)
}

func TestMCPAdapter_NoLongerImplementsAlerterBoundary(t *testing.T) {
	_, ok := any(NewMCPAdapter(nil)).(api.Alerter)
	assert.False(t, ok)
}
