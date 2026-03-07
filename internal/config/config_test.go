package config

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
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
	defer func() {
		_ = os.Unsetenv("XDG_CONFIG_HOME")
	}()

	// The expected path resolved by our logic
	expectedPath, _ := ResolveConfigPath()

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
		assert.False(t, cfg.Notifications.Enabled)
		// Verify default was preserved for missing field
		assert.Equal(t, 60, cfg.Notifications.SyncInterval)
	})
}

func TestConfig_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_STATE_HOME", tmpDir)

	cfg := DefaultConfig()
	
	// Test Save (Must ensure parent exists first)
	path, _ := ResolveConfigPath()
	require.NoError(t, EnsurePrivateDir(filepath.Dir(path)))
	require.NoError(t, cfg.Save())
	
	assert.FileExists(t, path)

	// Test Resolve helpers
	d, _ := ResolveDataDir()
	assert.Contains(t, d, tmpDir)
	
	s, _ := ResolveStateDir()
	assert.Contains(t, s, tmpDir)
	
	tp, _ := ResolveTracePath()
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
	info, _ := os.Stat(subDir)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())

	fInfo, _ := os.Stat(fPath)
	assert.Equal(t, os.FileMode(0o600), fInfo.Mode().Perm())
}
