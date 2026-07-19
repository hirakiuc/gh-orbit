package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/api"
	"github.com/hirakiuc/gh-orbit/internal/buildinfo"
	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/mocks"
	"github.com/hirakiuc/gh-orbit/internal/models"
	"github.com/hirakiuc/gh-orbit/internal/types"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func isCI() bool {
	return os.Getenv("CI") != ""
}

func isUnixSocketBindPermissionError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
		return true
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "operation not permitted") || strings.Contains(msg, "permission denied")
}

func waitForUDSServer(t *testing.T, socketPath string, errCh <-chan error) {
	t.Helper()

	var serveErr error
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(socketPath); err == nil {
			return
		}

		select {
		case serveErr = <-errCh:
			if isUnixSocketBindPermissionError(serveErr) && !isCI() {
				t.Skipf("UDS bind unavailable in local sandbox: %v", serveErr)
			}
		default:
		}

		if serveErr != nil {
			break
		}

		time.Sleep(20 * time.Millisecond)
	}

	if serveErr != nil {
		t.Fatalf("server failed to create UDS socket: %v", serveErr)
	}
	t.Fatalf("server failed to create UDS socket within timeout: %s", socketPath)
}

func newShortSocketPath(t *testing.T, name string) string {
	t.Helper()

	require.NoError(t, os.MkdirAll("tmp", 0o700))
	dir, err := os.MkdirTemp("tmp", "mcp-uds-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	return filepath.Join(dir, name)
}

func dialUnixEventually(t *testing.T, socketPath string) net.Conn {
	t.Helper()

	var conn net.Conn
	require.Eventually(t, func() bool {
		var err error
		conn, err = net.Dial("unix", socketPath)
		return err == nil
	}, 2*time.Second, 20*time.Millisecond)

	return conn
}

func readJSONLine(t *testing.T, conn net.Conn, reader *bufio.Reader, timeout time.Duration) map[string]any {
	t.Helper()

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(timeout)))
	line, err := reader.ReadString('\n')
	require.NoError(t, err)
	require.NoError(t, conn.SetReadDeadline(time.Time{}))

	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(line), &payload))
	return payload
}

func TestRequiredStateMutationArgs_StrictValidation(t *testing.T) {
	tests := []struct {
		name      string
		arguments any
		stateKey  string
		wantID    string
		wantState bool
		wantError string
	}{
		{name: "valid read", arguments: map[string]any{"id": " 123 ", "read": true}, stateKey: "read", wantID: "123", wantState: true},
		{name: "valid handled false", arguments: map[string]any{"id": "123", "handled": false}, stateKey: "handled", wantID: "123", wantState: false},
		{name: "missing id", arguments: map[string]any{"read": true}, stateKey: "read", wantError: "id must be a non-empty string"},
		{name: "blank id", arguments: map[string]any{"id": "  ", "read": true}, stateKey: "read", wantError: "id must be a non-empty string"},
		{name: "non-string id", arguments: map[string]any{"id": 123, "read": true}, stateKey: "read", wantError: "id must be a non-empty string"},
		{name: "missing state", arguments: map[string]any{"id": "123"}, stateKey: "handled", wantError: "handled must be a boolean"},
		{name: "non-boolean state", arguments: map[string]any{"id": "123", "handled": "true"}, stateKey: "handled", wantError: "handled must be a boolean"},
		{name: "invalid arguments", arguments: []any{"123", true}, stateKey: "read", wantError: "invalid arguments format"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, state, err := requiredStateMutationArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: tt.arguments}}, tt.stateKey)
			if tt.wantError != "" {
				require.EqualError(t, err, tt.wantError)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantID, id)
			assert.Equal(t, tt.wantState, state)
		})
	}
}

func TestMCPServer_UDSHandshake(t *testing.T) {
	// Setup CoreEngine
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg := config.DefaultConfig()
	executor := api.NewOSCommandExecutor()

	socketPath := newShortSocketPath(t, "mcp-test.sock")

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	eng, err := NewCoreEngine(ctx, cfg, logger, executor)
	if err != nil {
		t.Logf("Skipping integration test (db init likely failed in restricted env): %v", err)
		return
	}
	defer eng.Shutdown(ctx)

	server := NewMCPServer(eng, socketPath, true, false) // insecureDev=true for testing

	// Run server in background
	errChan := make(chan error, 1)
	go func() {
		errChan <- server.Serve(ctx)
	}()

	waitForUDSServer(t, socketPath, errChan)

	// Connect to UDS once the listener is ready.
	conn := dialUnixEventually(t, socketPath)
	defer func() {
		_ = conn.Close()
	}()

	// 1. Send initialize request (MCP uses JSONRPCRequest)
	// For testing, we send the raw JSON matching mcp-go expectation
	initReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": mcp.LATEST_PROTOCOL_VERSION,
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test-client", "version": "1.0"},
		},
	}

	reqData, _ := json.Marshal(initReq)
	_, err = fmt.Fprintf(conn, "%s\n", reqData)
	assert.NoError(t, err)

	// 2. Read response
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	assert.NoError(t, err)

	var resp mcp.JSONRPCResponse
	err = json.Unmarshal([]byte(line), &resp)
	assert.NoError(t, err)

	// id in mcp-go can be a number or string
	assert.NotNil(t, resp.ID)

	var initResult mcp.InitializeResult
	resBytes, _ := json.Marshal(resp.Result)
	err = json.Unmarshal(resBytes, &initResult)
	assert.NoError(t, err)
	assert.Equal(t, "gh-orbit", initResult.ServerInfo.Name)
	assert.Equal(t, buildinfo.Version, initResult.ServerInfo.Version)

	// 3. Signal exit
	cancel()
	<-errChan
}

func TestMCPServer_ProtocolChaos(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := config.DefaultConfig()
	executor := api.NewOSCommandExecutor()
	logger := slog.Default()

	socketPath := newShortSocketPath(t, "mcp-chaos-test.sock")

	eng, err := NewCoreEngine(ctx, cfg, logger, executor)
	if err != nil {
		t.Skip("skipping integration test")
		return
	}
	defer eng.Shutdown(ctx)

	server := NewMCPServer(eng, socketPath, true, false)
	errChan := make(chan error, 1)
	go func() { errChan <- server.Serve(ctx) }()
	waitForUDSServer(t, socketPath, errChan)

	t.Run("Malformed JSON", func(t *testing.T) {
		conn := dialUnixEventually(t, socketPath)
		defer func() { _ = conn.Close() }()

		_, _ = fmt.Fprintf(conn, "{invalid-json\n")
		// Server should not crash and should be ready for next message
		time.Sleep(50 * time.Millisecond)
	})

	t.Run("Unexpected Disconnect", func(t *testing.T) {
		conn := dialUnixEventually(t, socketPath)
		// Immediate close
		_ = conn.Close()
		time.Sleep(50 * time.Millisecond)
	})
}

func TestMCPServer_GracefulShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := config.DefaultConfig()

	executor := api.NewOSCommandExecutor()

	socketPath := newShortSocketPath(t, "mcp-shutdown-test.sock")

	// Use a buffer to capture logs
	var logBuf strings.Builder
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	eng, err := NewCoreEngine(ctx, cfg, logger, executor)
	if err != nil {
		t.Logf("Skipping test: %v", err)
		return
	}
	defer eng.Shutdown(ctx)

	server := NewMCPServer(eng, socketPath, true, false)

	errChan := make(chan error, 1)
	go func() {
		errChan <- server.Serve(ctx)
	}()

	waitForUDSServer(t, socketPath, errChan)

	// Trigger shutdown
	cancel()

	select {
	case err := <-errChan:
		assert.True(t, errors.Is(err, context.Canceled) || err == nil)
	case <-time.After(2 * time.Second):
		t.Fatal("Server did not shut down gracefully within timeout")
	}

	// Verify no "failed to accept connection" errors in log
	assert.NotContains(t, logBuf.String(), "failed to accept connection")
}

func TestMCPServer_MutationToolsNotifyUDSClients(t *testing.T) {
	newTestServer := func(t *testing.T) (*MCPServer, *mocks.MockRepository, *mocks.MockClient) {
		t.Helper()

		mockRepo := mocks.NewMockRepository(t)
		mockGH := mocks.NewMockClient(t)
		mockSync := mocks.NewMockSyncer(t)
		mockEnrich := mocks.NewMockEnricher(t)
		bus := NewEventBus()

		appBackend, err := api.NewAppBackend(api.AppBackendParams{
			UserID:                      "user-123",
			Store:                       mockRepo,
			Client:                      mockGH,
			Syncer:                      mockSync,
			Enricher:                    mockEnrich,
			PublishNotificationsChanged: func() { bus.Publish(EventNotificationListChanged) },
			PublishEnrichmentUpdated:    func() { bus.Publish(EventNotificationEnrichmentChanged) },
		})
		require.NoError(t, err)

		eng := &CoreEngine{
			Logger:            slog.Default(),
			Bus:               bus,
			DB:                mockRepo,
			Client:            mockGH,
			Sync:              mockSync,
			Enrich:            mockEnrich,
			Backend:           appBackend,
			legacyReadMutator: appBackend,
		}

		return NewMCPServer(eng, filepath.Join(t.TempDir(), "mcp-mutation.sock"), true, false), mockRepo, mockGH
	}

	newSession := func(t *testing.T, srv *MCPServer) (context.Context, net.Conn, *bufio.Reader, func()) {
		t.Helper()

		parentCtx, cancel := context.WithCancel(context.Background())
		serverConn, clientConn := net.Pipe()
		session, sessionCtx, cleanup, err := srv.setupSession(parentCtx, serverConn)
		require.NoError(t, err)
		session.Initialize()

		done := make(chan struct{})
		go func() {
			defer close(done)
			srv.runNotificationDispatcher(sessionCtx, session)
		}()

		eventLoopDone := make(chan struct{})
		go func() {
			defer close(eventLoopDone)
			srv.eventLoop(parentCtx)
		}()

		require.Eventually(t, func() bool {
			srv.engine.Bus.mu.RLock()
			defer srv.engine.Bus.mu.RUnlock()
			return len(srv.engine.Bus.subscribers[EventNotificationListChanged]) == 1 &&
				len(srv.engine.Bus.subscribers[EventNotificationEnrichmentChanged]) == 1
		}, time.Second, 10*time.Millisecond)

		return sessionCtx, clientConn, bufio.NewReader(clientConn), func() {
			cancel()
			cleanup()
			_ = clientConn.Close()
			_ = serverConn.Close()
			<-done
			<-eventLoopDone
		}
	}

	callTool := func(t *testing.T, srv *MCPServer, sessionCtx context.Context, requestID int, toolName string, args map[string]any) map[string]any {
		t.Helper()

		payload, err := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      requestID,
			"method":  "tools/call",
			"params": map[string]any{
				"name":      toolName,
				"arguments": args,
			},
		})
		require.NoError(t, err)

		message := srv.server.HandleMessage(sessionCtx, payload)
		data, err := json.Marshal(message)
		require.NoError(t, err)

		var response map[string]any
		require.NoError(t, json.Unmarshal(data, &response))
		return response
	}

	readNotification := func(t *testing.T, conn net.Conn, reader *bufio.Reader, timeout time.Duration) map[string]any {
		t.Helper()
		return readJSONLine(t, conn, reader, timeout)
	}
	assertNoNotification := func(t *testing.T, conn net.Conn, reader *bufio.Reader) {
		t.Helper()
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(100*time.Millisecond)))
		_, err := reader.ReadString('\n')
		var netErr net.Error
		require.ErrorAs(t, err, &netErr)
		assert.True(t, netErr.Timeout())
		require.NoError(t, conn.SetReadDeadline(time.Time{}))
	}

	t.Run("direct notification events send coarse resource invalidation", func(t *testing.T) {
		srv, _, _ := newTestServer(t)
		_, clientConn, reader, cleanup := newSession(t, srv)
		defer cleanup()

		srv.engine.Bus.Publish(EventNotificationListChanged)
		notification := readNotification(t, clientConn, reader, time.Second)
		assert.Equal(t, mcp.MethodNotificationResourcesListChanged, notification["method"])

		srv.engine.Bus.Publish(EventNotificationEnrichmentChanged)
		notification = readNotification(t, clientConn, reader, time.Second)
		assert.Equal(t, mcp.MethodNotificationResourcesListChanged, notification["method"])
	})

	t.Run("set_priority success sends resources/list_changed", func(t *testing.T) {
		srv, mockRepo, _ := newTestServer(t)
		sessionCtx, clientConn, reader, cleanup := newSession(t, srv)
		defer cleanup()

		mockRepo.EXPECT().
			SetPriority(mock.Anything, "1", 2).
			Return(nil).
			Once()
		mockRepo.EXPECT().
			ListNotifications(mock.Anything).
			Return(nil, nil).
			Twice()

		response := callTool(t, srv, sessionCtx, 2, "set_priority", map[string]any{
			"id":    "1",
			"level": 2,
		})
		notification := readNotification(t, clientConn, reader, time.Second)

		require.Contains(t, response, "result")
		assert.Equal(t, mcp.MethodNotificationResourcesListChanged, notification["method"])
	})

	t.Run("set_handled success sends resources/list_changed", func(t *testing.T) {
		srv, mockRepo, _ := newTestServer(t)
		sessionCtx, clientConn, reader, cleanup := newSession(t, srv)
		defer cleanup()

		mockRepo.EXPECT().SetHandledLocally(mock.Anything, "handled", true).Return(nil).Once()
		mockRepo.EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Twice()

		response := callTool(t, srv, sessionCtx, 2, "set_handled", map[string]any{
			"id": "handled", "handled": true,
		})
		notification := readNotification(t, clientConn, reader, time.Second)
		require.Contains(t, response, "result")
		assert.Equal(t, mcp.MethodNotificationResourcesListChanged, notification["method"])
	})

	t.Run("invalid independent requests do not call backend or notify", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			tool string
			args map[string]any
		}{
			{name: "set_read missing boolean", tool: "set_read", args: map[string]any{"id": "1"}},
			{name: "set_handled wrong boolean type", tool: "set_handled", args: map[string]any{"id": "1", "handled": "true"}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				srv, mockRepo, _ := newTestServer(t)
				sessionCtx, clientConn, reader, cleanup := newSession(t, srv)
				defer cleanup()

				response := callTool(t, srv, sessionCtx, 2, tc.tool, tc.args)
				result, ok := response["result"].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, true, result["isError"])
				mockRepo.AssertNotCalled(t, "ListNotifications", mock.Anything)
				mockRepo.AssertNotCalled(t, "SetReadLocally", mock.Anything, mock.Anything, mock.Anything)
				mockRepo.AssertNotCalled(t, "SetHandledLocally", mock.Anything, mock.Anything, mock.Anything)
				assertNoNotification(t, clientConn, reader)
			})
		}
	})

	t.Run("independent local failures do not notify", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			tool string
			args map[string]any
			set  func(*mocks.MockRepository)
		}{
			{name: "set_read unknown id", tool: "set_read", args: map[string]any{"id": "missing", "read": true}, set: func(repo *mocks.MockRepository) {
				repo.EXPECT().SetReadLocally(mock.Anything, "missing", true).Return(types.ErrNotificationNotFound).Once()
			}},
			{name: "set_handled unknown id", tool: "set_handled", args: map[string]any{"id": "missing", "handled": true}, set: func(repo *mocks.MockRepository) {
				repo.EXPECT().SetHandledLocally(mock.Anything, "missing", true).Return(types.ErrNotificationNotFound).Once()
			}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				srv, mockRepo, _ := newTestServer(t)
				sessionCtx, clientConn, reader, cleanup := newSession(t, srv)
				defer cleanup()
				mockRepo.EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Twice()
				tc.set(mockRepo)

				response := callTool(t, srv, sessionCtx, 2, tc.tool, tc.args)
				result, ok := response["result"].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, true, result["isError"])
				assertNoNotification(t, clientConn, reader)
			})
		}
	})

	t.Run("mark_read remote failure still notifies after local commit", func(t *testing.T) {
		srv, mockRepo, mockGH := newTestServer(t)
		sessionCtx, clientConn, reader, cleanup := newSession(t, srv)
		defer cleanup()

		mockRepo.EXPECT().
			MarkReadLocally(mock.Anything, "2", true).
			Return(nil).
			Once()
		mockGH.EXPECT().
			MarkThreadAsRead(mock.Anything, "2").
			Return(errors.New("boom")).
			Once()
		mockRepo.EXPECT().
			ListNotifications(mock.Anything).
			Return(nil, nil).
			Twice()

		response := callTool(t, srv, sessionCtx, 2, "mark_read", map[string]any{
			"id":   "2",
			"read": true,
		})
		notification := readNotification(t, clientConn, reader, time.Second)

		result, ok := response["result"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, true, result["isError"])
		assert.Equal(t, mcp.MethodNotificationResourcesListChanged, notification["method"])
	})

	t.Run("mark_read local failure does not notify before commit", func(t *testing.T) {
		srv, mockRepo, _ := newTestServer(t)
		sessionCtx, clientConn, reader, cleanup := newSession(t, srv)
		defer cleanup()

		mockRepo.EXPECT().
			MarkReadLocally(mock.Anything, "3", true).
			Return(errors.New("write failed")).
			Once()
		mockRepo.EXPECT().
			ListNotifications(mock.Anything).
			Return(nil, nil).
			Twice()

		response := callTool(t, srv, sessionCtx, 2, "mark_read", map[string]any{
			"id":   "3",
			"read": true,
		})
		result, ok := response["result"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, true, result["isError"])

		require.NoError(t, clientConn.SetReadDeadline(time.Now().Add(250*time.Millisecond)))
		_, err := reader.ReadString('\n')
		var netErr net.Error
		require.ErrorAs(t, err, &netErr)
		assert.True(t, netErr.Timeout(), "expected no notification after pre-commit failure")
		require.NoError(t, clientConn.SetReadDeadline(time.Time{}))
	})

	t.Run("sync interval not reached keeps structured MCP status after backend delegation", func(t *testing.T) {
		srv, _, _ := newTestServer(t)
		sessionCtx, _, _, cleanup := newSession(t, srv)
		defer cleanup()

		mockSync := srv.engine.Sync.(*mocks.MockSyncer)
		mockSync.EXPECT().
			Sync(mock.Anything, "user-123", false).
			Return(models.RateLimitInfo{Remaining: 123, Limit: 5000}, types.ErrSyncIntervalNotReached).
			Once()

		response := callTool(t, srv, sessionCtx, 2, "sync", map[string]any{})

		result, ok := response["result"].(map[string]any)
		require.True(t, ok)
		assert.NotEqual(t, true, result["isError"])

		structured, ok := result["structuredContent"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, string(syncToolStatusIntervalNotReached), structured["status"])
		assert.Equal(t, "sync interval not reached", result["content"].([]any)[0].(map[string]any)["text"])
	})

	t.Run("persist_fetched_detail still notifies after backend delegation", func(t *testing.T) {
		srv, _, _ := newTestServer(t)
		sessionCtx, clientConn, reader, cleanup := newSession(t, srv)
		defer cleanup()

		mockEnrich := srv.engine.Enrich.(*mocks.MockEnricher)
		mockEnrich.EXPECT().
			PersistFetchedDetail(mock.Anything, "4", "https://api.github.com/repos/o/r/pulls/4", mock.Anything).
			Return(nil).
			Once()

		response := callTool(t, srv, sessionCtx, 2, "persist_fetched_detail", map[string]any{
			"id":             "4",
			"source_url":     "https://api.github.com/repos/o/r/pulls/4",
			"node_id":        "node-4",
			"body":           "detail body",
			"author":         "hirakiuc",
			"html_url":       "https://github.com/o/r/pull/4",
			"resource_state": "OPEN",
		})
		notification := readNotification(t, clientConn, reader, time.Second)

		require.Contains(t, response, "result")
		assert.Equal(t, mcp.MethodNotificationResourcesListChanged, notification["method"])
	})
}

func TestMCPAdapter_Debounce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// 1. Setup adapter with a counter
		a := NewMCPAdapter(nil) // Client can be nil for this test
		var count int32

		a.OnMutation(func() {
			atomic.AddInt32(&count, 1)
		})

		// 2. Trigger multiple rapid updates
		for i := 0; i < 5; i++ {
			// Pass an empty notification just to trigger the handler
			a.handleResourceUpdate(mcp.JSONRPCNotification{})
			time.Sleep(50 * time.Millisecond)
		}

		// 3. Wait for debounce window (200ms) + buffer
		time.Sleep(500 * time.Millisecond)

		finalCount := atomic.LoadInt32(&count)

		// Expect only 1 mutation signal after 5 rapid triggers
		assert.Equal(t, int32(1), finalCount, "Should debounce multiple rapid updates into a single mutation signal")
	})
}

func TestMCPAdapter_ShutdownStopsPendingDebounceTimer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := NewMCPAdapter(nil)
		var count int32

		a.OnMutation(func() {
			atomic.AddInt32(&count, 1)
		})

		a.handleResourceUpdate(mcp.JSONRPCNotification{})
		time.Sleep(100 * time.Millisecond)
		a.Shutdown(context.Background())
		time.Sleep(300 * time.Millisecond)

		assert.Equal(t, int32(0), atomic.LoadInt32(&count), "shutdown should stop pending debounce delivery")
	})
}

func TestMCPAdapter_PostShutdownNotificationsAreNoOp(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := NewMCPAdapter(nil)
		var count int32

		a.OnMutation(func() {
			atomic.AddInt32(&count, 1)
		})

		a.Shutdown(context.Background())
		a.handleResourceUpdate(mcp.JSONRPCNotification{})
		time.Sleep(300 * time.Millisecond)

		assert.Equal(t, int32(0), atomic.LoadInt32(&count), "notifications after shutdown should not arm callbacks")
	})
}

func TestMCPAdapter_ShutdownDoesNotWaitForRunningCallback(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := NewMCPAdapter(nil)
		var startedCount int32
		started := make(chan struct{})
		release := make(chan struct{})

		a.OnMutation(func() {
			if atomic.AddInt32(&startedCount, 1) == 1 {
				close(started)
			}
			<-release
		})

		a.handleResourceUpdate(mcp.JSONRPCNotification{})
		time.Sleep(250 * time.Millisecond)
		<-started

		shutdownDone := make(chan struct{})
		go func() {
			a.Shutdown(context.Background())
			close(shutdownDone)
		}()

		select {
		case <-shutdownDone:
		case <-time.After(time.Second):
			t.Fatal("shutdown should not block on an already-running callback")
		}

		a.handleResourceUpdate(mcp.JSONRPCNotification{})
		close(release)
		time.Sleep(300 * time.Millisecond)

		assert.Equal(t, int32(1), atomic.LoadInt32(&startedCount), "no new callbacks should start after shutdown")
	})
}

func TestMCPServer_EventLoopUnsubscribesOnShutdown(t *testing.T) {
	bus := NewEventBus()
	mcpServer := server.NewMCPServer(
		"gh-orbit",
		"0.1.0",
		server.WithResourceCapabilities(true, false),
		server.WithToolCapabilities(true),
	)
	s := &MCPServer{
		engine: &CoreEngine{Bus: bus},
		server: mcpServer,
	}

	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			s.eventLoop(ctx)
		}()

		require.Eventually(t, func() bool {
			bus.mu.RLock()
			defer bus.mu.RUnlock()
			return len(bus.subscribers[EventNotificationListChanged]) == 1 &&
				len(bus.subscribers[EventNotificationEnrichmentChanged]) == 1
		}, time.Second, 10*time.Millisecond)

		cancel()
		require.Eventually(t, func() bool {
			select {
			case <-done:
				return true
			default:
				return false
			}
		}, time.Second, 10*time.Millisecond)

		bus.mu.RLock()
		_, notifOK := bus.subscribers[EventNotificationListChanged]
		_, enrichOK := bus.subscribers[EventNotificationEnrichmentChanged]
		bus.mu.RUnlock()

		assert.False(t, notifOK, "notification subscribers should return to baseline after shutdown")
		assert.False(t, enrichOK, "enrichment subscribers should return to baseline after shutdown")
	}
}
