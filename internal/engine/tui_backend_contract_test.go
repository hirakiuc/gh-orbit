package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hirakiuc/gh-orbit/internal/api"
	"github.com/hirakiuc/gh-orbit/internal/mocks"
	"github.com/hirakiuc/gh-orbit/internal/triage"
	"github.com/hirakiuc/gh-orbit/internal/types"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestTUIBackendContract_MarkReadSuccess(t *testing.T) {
	before := []triage.NotificationWithState{
		{Notification: triage.Notification{GitHubID: "notif-1"}},
	}
	after := []triage.NotificationWithState{
		{Notification: triage.Notification{GitHubID: "notif-1"}, State: triage.State{IsReadLocally: true}},
	}

	testCases := []struct {
		name    string
		backend func(t *testing.T) types.TUIBackend
	}{
		{
			name: "app backend",
			backend: func(t *testing.T) types.TUIBackend {
				mockRepo := mocks.NewMockRepository(t)
				mockSyncer := mocks.NewMockSyncer(t)
				mockEnricher := mocks.NewMockEnricher(t)
				mockClient := mocks.NewMockClient(t)

				mockRepo.EXPECT().ListNotifications(mock.Anything).Return(before, nil).Once()
				mockRepo.EXPECT().SetReadLocally(mock.Anything, "notif-1", true).Return(nil).Once()
				mockClient.EXPECT().MarkThreadAsRead(mock.Anything, "notif-1").Return(nil).Once()
				mockRepo.EXPECT().ListNotifications(mock.Anything).Return(after, nil).Once()

				backend, err := api.NewAppBackend(api.AppBackendParams{
					UserID:   "user-1",
					Store:    mockRepo,
					Client:   mockClient,
					Syncer:   mockSyncer,
					Enricher: mockEnricher,
				})
				require.NoError(t, err)
				return backend
			},
		},
		{
			name: "mcp adapter",
			backend: func(t *testing.T) types.TUIBackend {
				readCount := 0
				return NewMCPAdapter(&blockingMCPClient{
					readResource: func(ctx context.Context, request mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
						require.Equal(t, "gh-orbit://notifications/all", request.Params.URI)

						var snapshot []triage.NotificationWithState
						if readCount == 0 {
							snapshot = before
						} else {
							snapshot = after
						}
						readCount++

						payload, err := json.Marshal(snapshot)
						require.NoError(t, err)
						return &mcp.ReadResourceResult{
							Contents: []mcp.ResourceContents{
								mcp.TextResourceContents{
									URI:      request.Params.URI,
									MIMEType: "application/json",
									Text:     string(payload),
								},
							},
						}, nil
					},
					callTool: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
						require.Equal(t, "set_read", request.Params.Name)
						return mcp.NewToolResultText("Notification read state updated"), nil
					},
				})
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := tc.backend(t).SetRead(context.Background(), "notif-1", true)
			require.NoError(t, err)
			assert.Equal(t, types.MarkReadSuccess, result.Status)
			assert.Equal(t, after, result.Notifications)
			assert.Empty(t, result.Toast)
			assert.NoError(t, result.Err)
		})
	}
}

func TestTUIBackendContract_MarkReadRemoteFailure(t *testing.T) {
	before := []triage.NotificationWithState{
		{Notification: triage.Notification{GitHubID: "notif-2"}},
	}
	after := []triage.NotificationWithState{
		{Notification: triage.Notification{GitHubID: "notif-2"}, State: triage.State{IsReadLocally: true}},
	}
	remoteErr := errors.New("boom")

	testCases := []struct {
		name    string
		backend func(t *testing.T) types.TUIBackend
	}{
		{
			name: "app backend",
			backend: func(t *testing.T) types.TUIBackend {
				mockRepo := mocks.NewMockRepository(t)
				mockSyncer := mocks.NewMockSyncer(t)
				mockEnricher := mocks.NewMockEnricher(t)
				mockClient := mocks.NewMockClient(t)

				mockRepo.EXPECT().ListNotifications(mock.Anything).Return(before, nil).Once()
				mockRepo.EXPECT().SetReadLocally(mock.Anything, "notif-2", true).Return(nil).Once()
				mockClient.EXPECT().MarkThreadAsRead(mock.Anything, "notif-2").Return(remoteErr).Once()
				mockRepo.EXPECT().ListNotifications(mock.Anything).Return(after, nil).Once()

				backend, err := api.NewAppBackend(api.AppBackendParams{
					UserID:   "user-1",
					Store:    mockRepo,
					Client:   mockClient,
					Syncer:   mockSyncer,
					Enricher: mockEnricher,
				})
				require.NoError(t, err)
				return backend
			},
		},
		{
			name: "mcp adapter",
			backend: func(t *testing.T) types.TUIBackend {
				readCount := 0
				return NewMCPAdapter(&blockingMCPClient{
					readResource: func(ctx context.Context, request mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
						require.Equal(t, "gh-orbit://notifications/all", request.Params.URI)

						var snapshot []triage.NotificationWithState
						if readCount == 0 {
							snapshot = before
						} else {
							snapshot = after
						}
						readCount++

						payload, err := json.Marshal(snapshot)
						require.NoError(t, err)
						return &mcp.ReadResourceResult{
							Contents: []mcp.ResourceContents{
								mcp.TextResourceContents{
									URI:      request.Params.URI,
									MIMEType: "application/json",
									Text:     string(payload),
								},
							},
						}, nil
					},
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
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := tc.backend(t).SetRead(context.Background(), "notif-2", true)
			require.NoError(t, err)
			assert.Equal(t, types.MarkReadRemoteFailure, result.Status)
			assert.Equal(t, after, result.Notifications)
			assert.Equal(t, "Marked read locally; GitHub sync failed", result.Toast)
			require.Error(t, result.Err)
		})
	}
}

func TestTUIBackendContract_SetHandledSuccess(t *testing.T) {
	before := []triage.NotificationWithState{{Notification: triage.Notification{GitHubID: "handled"}, State: triage.State{IsReadLocally: true}}}
	after := []triage.NotificationWithState{{Notification: triage.Notification{GitHubID: "handled"}, State: triage.State{IsReadLocally: true, IsHandledLocally: true}}}

	testCases := []struct {
		name    string
		backend func(*testing.T) types.TUIBackend
	}{
		{name: "app backend", backend: func(t *testing.T) types.TUIBackend {
			repo := mocks.NewMockRepository(t)
			repo.EXPECT().ListNotifications(mock.Anything).Return(before, nil).Once()
			repo.EXPECT().SetHandledLocally(mock.Anything, "handled", true).Return(nil).Once()
			repo.EXPECT().ListNotifications(mock.Anything).Return(after, nil).Once()
			backend, err := api.NewAppBackend(api.AppBackendParams{
				UserID: "user", Store: repo, Syncer: mocks.NewMockSyncer(t), Enricher: mocks.NewMockEnricher(t),
			})
			require.NoError(t, err)
			return backend
		}},
		{name: "mcp adapter", backend: func(t *testing.T) types.TUIBackend {
			readCount := 0
			return NewMCPAdapter(&blockingMCPClient{
				readResource: func(_ context.Context, request mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
					snapshot := before
					if readCount > 0 {
						snapshot = after
					}
					readCount++
					payload, err := json.Marshal(snapshot)
					require.NoError(t, err)
					return &mcp.ReadResourceResult{Contents: []mcp.ResourceContents{mcp.TextResourceContents{URI: request.Params.URI, Text: string(payload)}}}, nil
				},
				callTool: func(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					require.Equal(t, "set_handled", request.Params.Name)
					return mcp.NewToolResultText("updated"), nil
				},
			})
		}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := tc.backend(t).SetHandled(context.Background(), "handled", true)
			require.NoError(t, err)
			assert.Equal(t, types.HandledUpdateSuccess, result.Status)
			assert.Equal(t, after, result.Notifications)
			assert.True(t, result.Notifications[0].IsReadLocally)
		})
	}
}

func TestTUIBackendContract_SetPrioritySuccess(t *testing.T) {
	before := []triage.NotificationWithState{
		{Notification: triage.Notification{GitHubID: "notif-3"}},
	}
	after := []triage.NotificationWithState{
		{Notification: triage.Notification{GitHubID: "notif-3"}, State: triage.State{Priority: 3}},
	}

	testCases := []struct {
		name    string
		backend func(t *testing.T) types.TUIBackend
	}{
		{
			name: "app backend",
			backend: func(t *testing.T) types.TUIBackend {
				mockRepo := mocks.NewMockRepository(t)
				mockSyncer := mocks.NewMockSyncer(t)
				mockEnricher := mocks.NewMockEnricher(t)

				mockRepo.EXPECT().ListNotifications(mock.Anything).Return(before, nil).Once()
				mockRepo.EXPECT().SetPriority(mock.Anything, "notif-3", 3).Return(nil).Once()
				mockRepo.EXPECT().ListNotifications(mock.Anything).Return(after, nil).Once()

				backend, err := api.NewAppBackend(api.AppBackendParams{
					UserID:   "user-1",
					Store:    mockRepo,
					Syncer:   mockSyncer,
					Enricher: mockEnricher,
				})
				require.NoError(t, err)
				return backend
			},
		},
		{
			name: "mcp adapter",
			backend: func(t *testing.T) types.TUIBackend {
				readCount := 0
				return NewMCPAdapter(&blockingMCPClient{
					readResource: func(ctx context.Context, request mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
						require.Equal(t, "gh-orbit://notifications/all", request.Params.URI)

						var snapshot []triage.NotificationWithState
						if readCount == 0 {
							snapshot = before
						} else {
							snapshot = after
						}
						readCount++

						payload, err := json.Marshal(snapshot)
						require.NoError(t, err)
						return &mcp.ReadResourceResult{
							Contents: []mcp.ResourceContents{
								mcp.TextResourceContents{
									URI:      request.Params.URI,
									MIMEType: "application/json",
									Text:     string(payload),
								},
							},
						}, nil
					},
					callTool: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
						require.Equal(t, "set_priority", request.Params.Name)
						return mcp.NewToolResultText("Priority updated"), nil
					},
				})
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := tc.backend(t).SetPriority(context.Background(), "notif-3", 3)
			require.NoError(t, err)
			assert.Equal(t, types.PriorityUpdateSuccess, result.Status)
			assert.Equal(t, after, result.Notifications)
			assert.Equal(t, "Priority set to High", result.Toast)
			assert.NoError(t, result.Err)
		})
	}
}

func TestTUIBackendContract_StartReviewWorkspaceUnsupportedInStandalone(t *testing.T) {
	backend, err := api.NewAppBackend(api.AppBackendParams{
		UserID:   "user-1",
		Store:    mocks.NewMockRepository(t),
		Client:   mocks.NewMockClient(t),
		Syncer:   mocks.NewMockSyncer(t),
		Enricher: mocks.NewMockEnricher(t),
	})
	require.NoError(t, err)

	err = backend.StartReviewWorkspace(context.Background(), types.ReviewWorkspaceStartRequest{
		Repository: types.ReviewWorkspaceRepository{
			Host:  "github.com",
			Owner: "acme",
			Name:  "orbit",
		},
		PullRequestNumber: 42,
	})
	require.ErrorIs(t, err, types.ErrReviewWorkspaceUnsupported)
}

func TestTUIBackendContract_StartReviewWorkspaceConnectedWritesStructuredRequest(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(reviewWorkspaceRequestDirectoryEnv, dir)

	backend := NewMCPAdapter(nil)
	request := types.ReviewWorkspaceStartRequest{
		Repository: types.ReviewWorkspaceRepository{
			Host:  "github.com",
			Owner: "acme",
			Name:  "orbit",
		},
		PullRequestNumber: 42,
	}

	require.NoError(t, backend.StartReviewWorkspace(context.Background(), request))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, ".json", filepath.Ext(entries[0].Name()))

	requestPath := filepath.Join(dir, filepath.Base(entries[0].Name()))
	// #nosec G304 -- the test reads the single file it just created inside t.TempDir().
	payload, err := os.ReadFile(requestPath)
	require.NoError(t, err)

	var got types.ReviewWorkspaceStartRequest
	require.NoError(t, json.Unmarshal(payload, &got))
	assert.Equal(t, request, got)
}
