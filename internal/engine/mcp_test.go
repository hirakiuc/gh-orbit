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
	"testing"
	"testing/synctest"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/api"
	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPServer_UDSHandshake(t *testing.T) {
	// Setup CoreEngine
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg := config.DefaultConfig()
	executor := api.NewOSCommandExecutor()

	// Ensure project-local tmp exists for socket
	cwd, _ := os.Getwd()
	tmpDir := filepath.Join(cwd, "../../tmp")
	_ = os.MkdirAll(tmpDir, 0o700)
	socketPath := filepath.Join(tmpDir, "mcp-test.sock")

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

	// Give server a moment to start
	time.Sleep(200 * time.Millisecond)

	// Connect to UDS
	conn, err := net.Dial("unix", socketPath)
	require.NoError(t, err)
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

	cwd, _ := os.Getwd()
	tmpDir := filepath.Join(cwd, "../../tmp")
	_ = os.MkdirAll(tmpDir, 0o700)
	socketPath := filepath.Join(tmpDir, "mcp-chaos-test.sock")

	eng, err := NewCoreEngine(ctx, cfg, logger, executor)
	if err != nil {
		t.Skip("skipping integration test")
		return
	}
	defer eng.Shutdown(ctx)

	server := NewMCPServer(eng, socketPath, true, false)
	go func() { _ = server.Serve(ctx) }()
	time.Sleep(100 * time.Millisecond)

	t.Run("Malformed JSON", func(t *testing.T) {
		conn, err := net.Dial("unix", socketPath)
		require.NoError(t, err)
		defer func() { _ = conn.Close() }()

		_, _ = fmt.Fprintf(conn, "{invalid-json\n")
		// Server should not crash and should be ready for next message
		time.Sleep(50 * time.Millisecond)
	})

	t.Run("Unexpected Disconnect", func(t *testing.T) {
		conn, err := net.Dial("unix", socketPath)
		require.NoError(t, err)
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

	cwd, _ := os.Getwd()
	tmpDir := filepath.Join(cwd, "../../tmp")
	_ = os.MkdirAll(tmpDir, 0o700)
	socketPath := filepath.Join(tmpDir, "mcp-shutdown-test.sock")

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

	// Give it a moment to start
	time.Sleep(100 * time.Millisecond)

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
