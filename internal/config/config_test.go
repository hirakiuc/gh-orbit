package config

import (
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
