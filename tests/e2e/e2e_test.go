package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLI_Bootstrap(t *testing.T) {
	// 1. Setup Mock GitHub API
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user/notifications":
			_ = json.NewEncoder(w).Encode([]interface{}{})
		case "/user":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 1, "login": "testuser"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(ts.Close)

	// 2. Prepare Environment
	tmpHome := t.TempDir()
	binPath := filepath.Join("..", "..", "bin", "gh-orbit")
	
	// Ensure binary exists
	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		t.Skip("gh-orbit binary not found in bin/. Run 'make build' first.")
	}

	// 3. Run 'gh-orbit doctor'
	cmd := exec.Command(binPath, "doctor") // #nosec G204: Trusted E2E test binary
	cmd.Env = append(os.Environ(),
		"HOME="+tmpHome,
		"GH_HOST=localhost",
		"GH_TOKEN=mock-token",
		"GH_ORBIT_SKIP_AUTH=1",
		"GH_ORBIT_API_URL="+ts.URL+"/",
		"XDG_CONFIG_HOME="+filepath.Join(tmpHome, ".config"),
		"XDG_DATA_HOME="+filepath.Join(tmpHome, ".local", "share"),
		"XDG_STATE_HOME="+filepath.Join(tmpHome, ".local", "state"),
	)

	output, err := cmd.CombinedOutput()
	t.Logf("Doctor Output:\n%s", string(output))
	require.NoError(t, err, "doctor command failed")

	// 4. Verify Files Created
	assert.FileExists(t, filepath.Join(tmpHome, ".config", "gh", "extensions", "gh-orbit", "config.yml"))
	// Data dir should exist, but DB might not be created by doctor if it uses in-memory for probe.
	// However, config.Load() should have created the data dir via ResolveDataDir and EnsurePrivateDir if we use it.
	assert.DirExists(t, filepath.Join(tmpHome, ".local", "share", "gh-orbit"))
}

func TestCLI_Sync(t *testing.T) {
	// 1. Setup Mock GitHub API
	notifID := "e2e-sync-123"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/notifications":
			resp := []map[string]interface{}{
				{
					"id": notifID,
					"updated_at": "2026-03-07T12:00:00Z",
					"reason": "mention",
					"repository": map[string]interface{}{"full_name": "owner/repo"},
					"subject": map[string]interface{}{
						"title": "E2E Test Notification",
						"url": "https://api.github.com/repos/owner/repo/issues/1",
						"type": "Issue",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case "/user":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 1, "login": "testuser"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(ts.Close)

	// 2. Prepare Environment
	tmpHome := t.TempDir()
	binPath := filepath.Join("..", "..", "bin", "gh-orbit")
	
	cmd := exec.Command(binPath, "--gh-orbit-test-mode") // #nosec G204: Trusted E2E test binary
	cmd.Env = append(os.Environ(),
		"HOME="+tmpHome,
		"GH_HOST=localhost",
		"GH_TOKEN=mock-token",
		"GH_ORBIT_SKIP_AUTH=1",
		"GH_ORBIT_API_URL="+ts.URL+"/",
		"XDG_CONFIG_HOME="+filepath.Join(tmpHome, ".config"),
		"XDG_DATA_HOME="+filepath.Join(tmpHome, ".local", "share"),
		"XDG_STATE_HOME="+filepath.Join(tmpHome, ".local", "state"),
	)

	// 3. Run non-interactive TUI
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "CLI execution failed: %s", string(output))

	// 4. Verify DB Persistence
	dbPath := filepath.Join(tmpHome, ".local", "share", "gh-orbit", "orbit.db")
	require.FileExists(t, dbPath)
}
