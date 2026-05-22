package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
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

func isSandboxUDSBindFailure(startErr error, stderr string, socketPath string) bool {
	if startErr == nil {
		return false
	}

	lowerStderr := strings.ToLower(stderr)
	lowerSocketPath := strings.ToLower(socketPath)

	if !strings.Contains(lowerStderr, "listen unix") {
		return false
	}

	if !strings.Contains(lowerStderr, lowerSocketPath) {
		return false
	}

	return isUnixSocketBindPermissionError(fmt.Errorf("%w: %s", startErr, stderr))
}

func newSocketPath(t *testing.T) string {
	t.Helper()

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	socketDir, err := os.MkdirTemp(filepath.Join(repoRoot, "tmp"), "e2e-sock-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })

	return filepath.Join(socketDir, "engine.sock")
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

	socketPath := newSocketPath(t)

	// #nosec G204: Trusted E2E test binary
	cmd := exec.CommandContext(ctx, binPath, "engine", "--socket", socketPath, "--insecure-dev")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Env = append(
		os.Environ(),
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
	var startErr error
	for i := 0; i < maxAttempts; i++ {
		if _, err := os.Stat(socketPath); err == nil {
			connected = true
			break
		}

		select {
		case startErr = <-errChan:
			if isSandboxUDSBindFailure(startErr, stderr.String(), socketPath) && !isCI() {
				cancel()
				t.Skipf("engine UDS bind unavailable in local sandbox: %v\n%s", startErr, strings.TrimSpace(stderr.String()))
			}
		default:
		}

		if startErr != nil {
			break
		}

		time.Sleep(100 * time.Millisecond)
	}

	if !connected {
		var details []string
		if startErr == nil {
			select {
			case startErr = <-errChan:
			default:
			}
		}
		if startErr != nil {
			details = append(details, fmt.Sprintf("process exited: %v", startErr))
		}
		if logs := strings.TrimSpace(stderr.String()); logs != "" {
			details = append(details, "stderr:\n"+logs)
		}
		cancel()
		if len(details) > 0 {
			t.Fatalf("Engine failed to create UDS socket within timeout\n%s", strings.Join(details, "\n"))
		}
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

func TestIsCI(t *testing.T) {
	t.Setenv("CI", "")
	assert.False(t, isCI())

	t.Setenv("CI", "1")
	assert.True(t, isCI())
}

func TestIsUnixSocketBindPermissionError(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil",
			err:  nil,
			want: false,
		},
		{
			name: "os err permission",
			err:  os.ErrPermission,
			want: true,
		},
		{
			name: "syscall eperm",
			err:  syscall.EPERM,
			want: true,
		},
		{
			name: "wrapped permission denied message",
			err:  fmt.Errorf("listen unix: %w", errors.New("permission denied")),
			want: true,
		},
		{
			name: "wrapped operation not permitted message",
			err:  fmt.Errorf("listen unix: %w", errors.New("operation not permitted")),
			want: true,
		},
		{
			name: "unrelated startup failure",
			err:  errors.New("unexpected EOF"),
			want: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, isUnixSocketBindPermissionError(tc.err))
		})
	}
}

func TestIsSandboxUDSBindFailure(t *testing.T) {
	t.Parallel()

	socketPath := "/tmp/e2e-sock-123/engine.sock"
	startErr := errors.New("exit status 1")

	testCases := []struct {
		name      string
		startErr  error
		stderr    string
		socket    string
		wantMatch bool
	}{
		{
			name:      "known uds bind denial",
			startErr:  startErr,
			stderr:    fmt.Sprintf("Error: failed to listen on UDS %s: listen unix %s: bind: operation not permitted", socketPath, socketPath),
			socket:    socketPath,
			wantMatch: true,
		},
		{
			name:      "nil startup error",
			startErr:  nil,
			stderr:    fmt.Sprintf("listen unix %s: bind: operation not permitted", socketPath),
			socket:    socketPath,
			wantMatch: false,
		},
		{
			name:      "unrelated stderr",
			startErr:  startErr,
			stderr:    "unexpected EOF while starting engine",
			socket:    socketPath,
			wantMatch: false,
		},
		{
			name:      "missing socket path in stderr",
			startErr:  startErr,
			stderr:    "listen unix /tmp/other.sock: bind: operation not permitted",
			socket:    socketPath,
			wantMatch: false,
		},
		{
			name:      "non permission startup failure",
			startErr:  startErr,
			stderr:    fmt.Sprintf("listen unix %s: address already in use", socketPath),
			socket:    socketPath,
			wantMatch: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.wantMatch, isSandboxUDSBindFailure(tc.startErr, tc.stderr, tc.socket))
		})
	}
}
