package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testEngine struct {
	t          *testing.T
	cmd        *exec.Cmd
	socketPath string
	ctx        context.Context
	cancel     context.CancelFunc
	errChan    chan error
}

func spawnEngine(t *testing.T, tmpHome string) *testEngine {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	binPath, err := filepath.Abs(filepath.Join("..", "..", "bin", "gh-orbit"))
	require.NoError(t, err)

	// Ensure binary exists
	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		t.Fatalf("gh-orbit binary not found at %s. Run 'make go/build' first.", binPath)
	}

	socketPath := filepath.Join(t.TempDir(), "engine.sock")

	// #nosec G204: Trusted E2E test binary
	cmd := exec.CommandContext(ctx, binPath, "engine", "--socket", socketPath, "--insecure-dev")
	cmd.Env = append(os.Environ(),
		"HOME="+tmpHome,
		"GH_TOKEN=mock-token",
		"GH_ORBIT_SKIP_AUTH=1",
		"XDG_CONFIG_HOME="+filepath.Join(tmpHome, ".config"),
		"XDG_DATA_HOME="+filepath.Join(tmpHome, ".local", "share"),
		"XDG_STATE_HOME="+filepath.Join(tmpHome, ".local", "state"),
	)

	errChan := make(chan error, 1)
	go func() {
		errChan <- cmd.Run()
	}()

	// Wait for socket to become available (Retry loop)
	maxAttempts := 20
	var connected bool
	for i := 0; i < maxAttempts; i++ {
		if _, err := os.Stat(socketPath); err == nil {
			connected = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !connected {
		cancel()
		t.Fatal("Engine failed to create UDS socket within timeout")
	}

	return &testEngine{
		t:          t,
		cmd:        cmd,
		socketPath: socketPath,
		ctx:        ctx,
		cancel:     cancel,
		errChan:    errChan,
	}
}

func (e *testEngine) stop() {
	e.cancel()
	select {
	case <-e.errChan:
		// expected termination
	case <-time.After(2 * time.Second):
		e.t.Log("Engine did not stop gracefully, killing...")
		_ = e.cmd.Process.Kill()
	}
}

func TestEngine_Lifecycle(t *testing.T) {
	tmpHome := t.TempDir()
	engine := spawnEngine(t, tmpHome)
	defer engine.stop()

	assert.FileExists(t, engine.socketPath)
}

func TestEngine_MCP_Handshake(t *testing.T) {
	tmpHome := t.TempDir()
	engine := spawnEngine(t, tmpHome)
	defer engine.stop()

	// Connect to UDS
	conn, err := net.Dial("unix", engine.socketPath)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	// Send JSON-RPC Initialize
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "e2e-test", "version": "1.0.0"},
		},
	}
	data, _ := json.Marshal(req)
	_, err = fmt.Fprintf(conn, "%s\n", string(data))
	require.NoError(t, err)

	// Read Response
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	require.NoError(t, err)

	var resp map[string]any
	err = json.Unmarshal([]byte(line), &resp)
	require.NoError(t, err)

	// Verify server info
	result := resp["result"].(map[string]any)
	serverInfo := result["serverInfo"].(map[string]any)
	assert.Equal(t, "gh-orbit", serverInfo["name"])
}

func TestEngine_MCP_Tools(t *testing.T) {
	tmpHome := t.TempDir()
	engine := spawnEngine(t, tmpHome)
	defer engine.stop()

	conn, err := net.Dial("unix", engine.socketPath)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	// List Tools
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
		"params":  map[string]any{},
	}
	data, _ := json.Marshal(req)
	_, _ = fmt.Fprintf(conn, "%s\n", string(data))

	reader := bufio.NewReader(conn)
	line, _ := reader.ReadString('\n')

	var resp map[string]any
	_ = json.Unmarshal([]byte(line), &resp)

	result := resp["result"].(map[string]any)
	tools := result["tools"].([]any)

	// Verify we have our core tools
	foundSync := false
	for _, it := range tools {
		tool := it.(map[string]any)
		if tool["name"] == "sync" {
			foundSync = true
			break
		}
	}
	assert.True(t, foundSync, "Engine should expose 'sync' tool via MCP")
}
