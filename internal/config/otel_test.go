package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupOTel(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmpDir)

	ctx := context.Background()
	version := "1.0.0"

	tp, cleanup, err := SetupOTel(ctx, version)
	require.NoError(t, err)
	require.NotNil(t, tp)
	require.NotNil(t, cleanup)

	// Ensure cleanup function runs without error
	defer cleanup()

	// Verify trace file exists
	stateDir := filepath.Join(tmpDir, "gh-orbit")
	tracePath := filepath.Join(stateDir, "orbit.traces.json")
	assert.FileExists(t, tracePath)

	// Verify directory permissions
	info, err := os.Stat(stateDir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())

	// Verify file permissions
	fInfo, err := os.Stat(tracePath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fInfo.Mode().Perm())
}
