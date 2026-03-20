package buildinfo

import (
	"fmt"
	"runtime"
)

var (
	// Version is the application version (e.g., v1.0.0).
	Version = "dev"
	// Commit is the git commit hash at build time.
	Commit = "unknown"
	// Date is the build date in ISO 8601 format.
	Date = "unknown"
)

// FullVersion returns a formatted string containing all build metadata.
func FullVersion() string {
	return fmt.Sprintf("%s (%s) build %s %s/%s", Version, Commit, Date, runtime.GOOS, runtime.GOARCH)
}
