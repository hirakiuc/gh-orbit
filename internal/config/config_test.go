package config

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_Validate(t *testing.T) {
	t.Run("Valid Default Config", func(t *testing.T) {
		cfg := DefaultConfig()
		require.NoError(t, cfg.Validate())
	})

	t.Run("Invalid Sync Interval (Too Low)", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Notifications.SyncInterval = 5
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "notifications.sync_interval must be between 10 and 3600")
	})

	t.Run("Invalid Sync Interval (Too High)", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Notifications.SyncInterval = 4000
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "notifications.sync_interval must be between 10 and 3600")
	})

	t.Run("Invalid MaxVisibleAgeDays (Too Low)", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Notifications.MaxVisibleAgeDays = -1
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "notifications.max_visible_age_days must be between 0 and 3650 days")
	})

	t.Run("Invalid MaxVisibleAgeDays (Too High)", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Notifications.MaxVisibleAgeDays = 3651
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "notifications.max_visible_age_days must be between 0 and 3650 days")
	})

	t.Run("Invalid DebounceMS", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Enrichment.DebounceMS = 10
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "enrichment.debounce_ms must be between 50 and 5000ms")
	})

	t.Run("Invalid Concurrency", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Enrichment.Concurrency = 0
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "enrichment.concurrency must be between 1 and 10")
	})
}

func TestConfig_Load_Strictness(t *testing.T) {
	tmpDir := t.TempDir()

	// Setup environment override
	err := os.Setenv("XDG_CONFIG_HOME", tmpDir)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Unsetenv("XDG_CONFIG_HOME")
	})

	// The expected path resolved by our logic
	expectedPath, err := ResolveConfigPath()
	require.NoError(t, err)

	t.Run("Catch Unknown Fields (Typo)", func(t *testing.T) {
		content := `
version: 1
notifications:
  enabled: true
  sync_intreval: 30 # TYPO
`
		err := os.MkdirAll(filepath.Dir(expectedPath), 0o700)
		require.NoError(t, err)

		err = os.WriteFile(expectedPath, []byte(content), 0o600)
		require.NoError(t, err)

		_, err = Load()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "check for typos")
	})

	t.Run("Successful Load with Safe Defaults", func(t *testing.T) {
		// YAML only provides version and enabled
		content := `
version: 1
notifications:
  enabled: false
`
		// Overwrite the file for this test
		err := os.WriteFile(expectedPath, []byte(content), 0o600)
		require.NoError(t, err)

		cfg, err := Load()
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.False(t, cfg.Notifications.Enabled)
		// Verify default was preserved for missing field
		assert.Equal(t, 60, cfg.Notifications.SyncInterval)
		assert.Equal(t, 365, cfg.Notifications.MaxVisibleAgeDays)
		// Verify TUI defaults when section is missing
		assert.False(t, cfg.TUI.AutoReadOnOpen)
	})

	t.Run("Successful Load with Keybinding Overrides", func(t *testing.T) {
		content := `
version: 1
keys:
  sync: ["s"]
  quit: ["escape"]
`
		err := os.WriteFile(expectedPath, []byte(content), 0o600)
		require.NoError(t, err)

		cfg, err := Load()
		require.NoError(t, err)
		require.NotNil(t, cfg)

		// Verify overrides
		assert.Equal(t, []string{"s"}, cfg.Keys.Sync)
		assert.Equal(t, []string{"escape"}, cfg.Keys.Quit)

		// Verify fallback to defaults for other keys
		assert.Equal(t, []string{"r"}, DefaultConfig().Keys.Sync) // Default was 'r'
		assert.Equal(t, []string{"m"}, cfg.Keys.ToggleRead)       // Still 'm'
	})
}

func TestConfig_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_STATE_HOME", tmpDir)

	cfg := DefaultConfig()

	// Test Save (Must ensure parent exists first)
	path, err := ResolveConfigPath()
	require.NoError(t, err)
	require.NoError(t, EnsurePrivateDir(filepath.Dir(path)))
	require.NoError(t, cfg.Save())

	assert.FileExists(t, path)

	// Test Resolve helpers
	d, err := ResolveDataDir()
	require.NoError(t, err)
	assert.Contains(t, d, tmpDir)

	s, err := ResolveStateDir()
	require.NoError(t, err)
	assert.Contains(t, s, tmpDir)

	tp, err := ResolveTracePath()
	require.NoError(t, err)
	assert.Contains(t, tp, tmpDir)
}

func TestConfig_AuditPermissions(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	// Create dir with loose permissions
	subDir := filepath.Join(tmpDir, "loose")
	require.NoError(t, os.MkdirAll(subDir, 0o777)) // #nosec G301: Intentional loose perms for audit test

	fPath := filepath.Join(subDir, "file.txt")
	require.NoError(t, os.WriteFile(fPath, []byte("data"), 0o666)) // #nosec G306: Intentional loose perms for audit test

	// Audit
	require.NoError(t, AuditPermissions(ctx, slog.Default(), tmpDir))

	// Verify hardening
	info, err := os.Stat(subDir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())

	fInfo, err := os.Stat(fPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fInfo.Mode().Perm())
}

func TestResolvePaths_Sandbox(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Sandbox path resolution is only applicable on Darwin")
	}

	t.Run("Standard (XDG) Resolution", func(t *testing.T) {
		t.Setenv("APP_SANDBOX_CONTAINER_ID", "")
		t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")

		path, err := ResolveConfigPath()
		require.NoError(t, err)
		assert.Contains(t, path, "/tmp/xdg")
	})

	t.Run("Sandboxed Resolution", func(t *testing.T) {
		t.Setenv("APP_SANDBOX_CONTAINER_ID", "com.hirakiuc.gh-orbit.cockpit")

		// In sandbox, ResolveStateDir and ResolveDataDir should point to Library/Group Containers
		state, err := ResolveStateDir()
		require.NoError(t, err)
		assert.Contains(t, state, "Library/Group Containers/"+AppGroupID)

		data, err := ResolveDataDir()
		require.NoError(t, err)
		assert.Contains(t, data, "Library/Group Containers/"+AppGroupID)

		// Config should point to Library/Application Support
		config, err := ResolveConfigPath()
		require.NoError(t, err)
		assert.Contains(t, config, "Library/Application Support/gh-orbit")
	})
}
