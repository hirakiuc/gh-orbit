//go:build darwin

package transport

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVerifyCodeSignatureAtPath(t *testing.T) {
	tempDir := t.TempDir()

	allowed := signedFixture(t, tempDir, "gh-orbit-cli")
	require.NoError(t, verifyCodeSignatureAtPath(allowed))

	denied := signedFixture(t, tempDir, "com.example.unrelated")
	require.Error(t, verifyCodeSignatureAtPath(denied))
}

func signedFixture(t *testing.T, directory, identifier string) string {
	t.Helper()

	path := filepath.Join(directory, identifier)
	contents, err := os.ReadFile("/usr/bin/true")
	require.NoError(t, err)
	// #nosec G703: The path is derived from t.TempDir and a fixed test identity.
	require.NoError(t, os.WriteFile(path, contents, 0o600))
	// #nosec G302: The copied test executable must be executable for codesign.
	require.NoError(t, os.Chmod(path, 0o700))

	// #nosec G204: The test controls the fixture path and identity.
	command := exec.Command("codesign", "-f", "-s", "-", "-i", identifier, path)
	require.NoError(t, command.Run())
	return path
}
