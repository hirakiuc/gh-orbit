package config

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
)

var tokenRegex = regexp.MustCompile(`(ghp_|github_pat_|gho_|ghs_|ghr_)[a-zA-Z0-9]{36,}`)

// SetupLogger initializes a structured slog logger with redaction and buffered file output.
// It uses levelVar to allow dynamic, thread-safe log level updates.
func SetupLogger(level *slog.LevelVar) (*slog.Logger, func() error, error) {
	path, err := resolveLogPath()
	if err != nil {
		return nil, nil, err
	}

	// 1. Log Rotation Guard (10MB)
	flags := os.O_CREATE | os.O_APPEND | os.O_WRONLY
	if info, err := os.Stat(path); err == nil {
		if info.Size() > 10*1024*1024 {
			// Atomically truncate if too large
			flags |= os.O_TRUNC
		}
	}

	// Create parent directory with 0700 permissions
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	file, err := os.OpenFile(path, flags, 0o600) // #nosec G304: Path is internally resolved following XDG specs
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open log file: %w", err)
	}

	// Buffered writer for performance
	bufferedWriter := bufio.NewWriter(file)

	// Redacting handler options
	opts := &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Value.Kind() == slog.KindString {
				val := a.Value.String()
				if tokenRegex.MatchString(val) {
					return slog.String(a.Key, tokenRegex.ReplaceAllString(val, "<REDACTED>"))
				}
			}
			return a
		},
	}

	handler := slog.NewJSONHandler(bufferedWriter, opts)
	logger := slog.New(handler)

	cleanup := func() error {
		if err := bufferedWriter.Flush(); err != nil {
			_ = file.Close()
			return err
		}
		return file.Close()
	}

	return logger, cleanup, nil
}

func resolveLogPath() (string, error) {
	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		// On macOS/Linux fallback to ~/.local/state
		stateHome = filepath.Join(home, ".local", "state")
	}

	return filepath.Join(stateHome, "gh-orbit", "orbit.log"), nil
}
