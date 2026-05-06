package engine

import (
	"context"
	"testing"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/models"
	"github.com/hirakiuc/gh-orbit/internal/types"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type blockingMCPClient struct {
	callTool func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error)
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

	_, err := adapter.Sync(context.Background(), "user-1", true)
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

	_, err := adapter.Sync(parentCtx, "user-1", true)
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

	rl, err := adapter.Sync(context.Background(), "user-1", true)
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

	rl, err := adapter.Sync(context.Background(), "user-1", false)
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

	rl, err := adapter.Sync(context.Background(), "user-1", true)
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

	_, err := adapter.Sync(context.Background(), "user-1", true)
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

	_, err := adapter.Sync(context.Background(), "user-1", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `invalid sync tool status "unknown"`)
}
