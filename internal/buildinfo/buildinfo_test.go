package buildinfo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFullVersion(t *testing.T) {
	// With default values
	Version = "dev"
	Commit = "unknown"
	Date = "unknown"

	v := FullVersion()
	assert.Contains(t, v, "dev")
	assert.Contains(t, v, "unknown")
}
