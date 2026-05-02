package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"

	"github.com/hirakiuc/gh-orbit/internal/mocks"
	"github.com/hirakiuc/gh-orbit/internal/models"
	"github.com/hirakiuc/gh-orbit/internal/triage"
	mcpclient "github.com/mark3labs/mcp-go/client"
	clienttransport "github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func newInMemoryMCPClient(t *testing.T, srv *MCPServer) *mcpclient.Client {
	t.Helper()

	serverReader, clientWriter := io.Pipe()
	clientReader, serverWriter := io.Pipe()

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		stdioServer := mcpserver.NewStdioServer(srv.server)
		_ = stdioServer.Listen(ctx, serverReader, serverWriter)
	}()

	var logBuf bytes.Buffer
	transport := clienttransport.NewIO(clientReader, clientWriter, io.NopCloser(&logBuf))
	require.NoError(t, transport.Start(ctx))

	client := mcpclient.NewClient(transport)
	var initReq mcp.InitializeRequest
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	_, err := client.Initialize(ctx, initReq)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = transport.Close()
		cancel()
		_ = serverWriter.Close()
		_ = serverReader.Close()
		_ = clientWriter.Close()
		_ = clientReader.Close()
		wg.Wait()
	})

	return client
}

func newTestMCPServerForEnrichment(t *testing.T) (*MCPServer, *mocks.MockRepository, *mocks.MockEnricher) {
	t.Helper()

	mockRepo := mocks.NewMockRepository(t)
	mockEnrich := mocks.NewMockEnricher(t)
	mockGH := mocks.NewMockGitHubClient(t)
	mockSync := mocks.NewMockSyncer(t)

	engine := &CoreEngine{
		DB:     mockRepo,
		Enrich: mockEnrich,
		Client: mockGH,
		Sync:   mockSync,
	}

	return NewMCPServer(engine, "/tmp/test.sock", true, false), mockRepo, mockEnrich
}

func decodeTextResult[T any](t *testing.T, resp *mcp.CallToolResult) T {
	t.Helper()

	require.NotNil(t, resp)
	require.False(t, resp.IsError)
	require.NotEmpty(t, resp.Content)

	text, ok := resp.Content[0].(mcp.TextContent)
	require.True(t, ok)

	var out T
	require.NoError(t, json.Unmarshal([]byte(text.Text), &out))
	return out
}

func TestMCPServer_EnrichmentTools(t *testing.T) {
	srv, mockRepo, mockEnrich := newTestMCPServerForEnrichment(t)
	client := newInMemoryMCPClient(t, srv)

	t.Run("fetch_detail returns a real enrichment payload", func(t *testing.T) {
		expected := models.EnrichmentResult{
			SubjectNodeID:    "node-1",
			Body:             "detail body",
			HTMLURL:          "https://github.com/o/r/pull/1",
			Author:           "hirakiuc",
			ResourceState:    "OPEN",
			ResourceSubState: "APPROVED",
		}
		mockEnrich.EXPECT().
			FetchDetail(mock.Anything, "https://api.github.com/repos/o/r/pulls/1", "PullRequest", true).
			Return(expected, nil).
			Once()

		resp, err := client.CallTool(context.Background(), mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "fetch_detail",
				Arguments: map[string]any{
					"url":          "https://api.github.com/repos/o/r/pulls/1",
					"subject_type": "PullRequest",
					"force":        true,
				},
			},
		})
		require.NoError(t, err)

		got := decodeTextResult[models.EnrichmentResult](t, resp)
		assert.Equal(t, expected, got)
	})

	t.Run("fetch_hybrid_batch returns a node-id keyed map", func(t *testing.T) {
		notifications := []triage.NotificationWithState{
			{
				Notification: triage.Notification{
					GitHubID:      "1",
					SubjectNodeID: "node-1",
					SubjectType:   triage.SubjectPullRequest,
				},
			},
		}
		expected := map[string]models.EnrichmentResult{
			"node-1": {
				ResourceState:    "MERGED",
				ResourceSubState: "APPROVED",
			},
		}
		mockEnrich.EXPECT().
			FetchHybridBatch(mock.Anything, notifications, true).
			Return(expected).
			Once()

		resp, err := client.CallTool(context.Background(), mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "fetch_hybrid_batch",
				Arguments: map[string]any{
					"notifications": notifications,
					"force":         true,
				},
			},
		})
		require.NoError(t, err)

		got := decodeTextResult[map[string]models.EnrichmentResult](t, resp)
		assert.Equal(t, expected, got)
	})

	t.Run("enrich_notification persists enriched fields", func(t *testing.T) {
		mockRepo.EXPECT().
			EnrichNotification(mock.Anything, "1", "node-1", "detail body", "hirakiuc", "https://github.com/o/r/pull/1", "OPEN", "APPROVED").
			Return(nil).
			Once()

		resp, err := client.CallTool(context.Background(), mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "enrich_notification",
				Arguments: map[string]any{
					"id":                 "1",
					"node_id":            "node-1",
					"body":               "detail body",
					"author":             "hirakiuc",
					"html_url":           "https://github.com/o/r/pull/1",
					"resource_state":     "OPEN",
					"resource_sub_state": "APPROVED",
				},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.False(t, resp.IsError)
	})
}

func TestMCPAdapter_FetchDetail_ThroughMCP(t *testing.T) {
	srv, _, mockEnrich := newTestMCPServerForEnrichment(t)
	client := newInMemoryMCPClient(t, srv)
	adapter := NewMCPAdapter(client)

	expected := models.EnrichmentResult{
		SubjectNodeID: "node-2",
		Body:          "adapter detail body",
		Author:        "reviewer",
		HTMLURL:       "https://github.com/o/r/issues/2",
	}
	mockEnrich.EXPECT().
		FetchDetail(mock.Anything, "https://api.github.com/repos/o/r/issues/2", "Issue", false).
		Return(expected, nil).
		Once()

	got, err := adapter.FetchDetail(context.Background(), "https://api.github.com/repos/o/r/issues/2", "Issue", false)
	require.NoError(t, err)
	assert.Equal(t, expected, got)
}

func TestMCPAdapter_FetchHybridBatch_ThroughMCP(t *testing.T) {
	srv, _, mockEnrich := newTestMCPServerForEnrichment(t)
	client := newInMemoryMCPClient(t, srv)
	adapter := NewMCPAdapter(client)

	notifications := []triage.NotificationWithState{
		{
			Notification: triage.Notification{
				GitHubID:      "2",
				SubjectNodeID: "node-2",
				SubjectType:   triage.SubjectIssue,
			},
		},
	}
	expected := map[string]models.EnrichmentResult{
		"node-2": {
			ResourceState:    "CLOSED",
			ResourceSubState: "COMPLETED",
		},
	}
	mockEnrich.EXPECT().
		FetchHybridBatch(mock.Anything, notifications, false).
		Return(expected).
		Once()

	got := adapter.FetchHybridBatch(context.Background(), notifications, false)
	assert.Equal(t, expected, got)
}

func TestMCPAdapter_SingleItemEnrichmentFlow_ThroughMCP(t *testing.T) {
	srv, mockRepo, mockEnrich := newTestMCPServerForEnrichment(t)
	client := newInMemoryMCPClient(t, srv)
	adapter := NewMCPAdapter(client)

	expected := models.EnrichmentResult{
		SubjectNodeID:    "node-3",
		Body:             "persisted detail body",
		Author:           "octocat",
		HTMLURL:          "https://github.com/o/r/pull/3",
		ResourceState:    "OPEN",
		ResourceSubState: "REVIEW_REQUIRED",
	}
	mockEnrich.EXPECT().
		FetchDetail(mock.Anything, "https://api.github.com/repos/o/r/pulls/3", "PullRequest", true).
		Return(expected, nil).
		Once()
	mockRepo.EXPECT().
		EnrichNotification(mock.Anything, "notif-3", "node-3", "persisted detail body", "octocat", "https://github.com/o/r/pull/3", "OPEN", "REVIEW_REQUIRED").
		Return(nil).
		Once()

	result, err := adapter.FetchDetail(context.Background(), "https://api.github.com/repos/o/r/pulls/3", "PullRequest", true)
	require.NoError(t, err)
	require.Equal(t, expected, result)

	err = adapter.EnrichNotification(context.Background(), "notif-3", result.SubjectNodeID, result.Body, result.Author, result.HTMLURL, result.ResourceState, result.ResourceSubState)
	require.NoError(t, err)
}
