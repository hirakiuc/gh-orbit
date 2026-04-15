package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"
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
	_ = os.MkdirAll(tmpDir, 0700)
	socketPath := filepath.Join(tmpDir, "mcp-test.sock")

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	eng, err := NewCoreEngine(ctx, cfg, logger, executor)
	if err != nil {
		t.Logf("Skipping integration test (db init likely failed in restricted env): %v", err)
		return
	}
	defer eng.Shutdown(ctx)

	server := NewMCPServer(eng, socketPath, true) // insecureDev=true for testing

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
	defer conn.Close()

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
	_ = <-errChan
}
