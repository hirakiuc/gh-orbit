package config

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"

	"go.opentelemetry.io/otel/trace"
)

var tokenRegex = regexp.MustCompile(`(ghp_|github_pat_|gho_|ghs_|ghr_)[a-zA-Z0-9]{36,}`)

// SetupLogger initializes a structured slog logger with redaction and buffered file output.
// It uses levelVar to allow dynamic, thread-safe log level updates.
func SetupLogger(level *slog.LevelVar) (*slog.Logger, func() error, error) {
	stateDir, err := ResolveStateDir()
	if err != nil {
		return nil, nil, err
	}
	path := filepath.Join(stateDir, "orbit.log")

	// 1. Log Rotation Guard (10MB)
	flags := os.O_CREATE | os.O_APPEND | os.O_WRONLY
	if info, err := os.Stat(path); err == nil {
		if info.Size() > 10*1024*1024 {
			// Atomically truncate if too large
			flags |= os.O_TRUNC
		}
	}

	// Create parent directory with strict permissions
	if err := EnsurePrivateDir(filepath.Dir(path)); err != nil {
		return nil, nil, fmt.Errorf("failed to secure log directory: %w", err)
	}

	file, err := os.OpenFile(path, flags, 0o600) // #nosec G304: Path is internally resolved following XDG specs
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open log file: %w", err)
	}

	// Buffered writer for performance
	bufferedWriter := bufio.NewWriter(file)

	// Custom handler to inject trace correlation
	opts := &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// 1. Token Redaction
			if a.Value.Kind() == slog.KindString {
				val := a.Value.String()
				if tokenRegex.MatchString(val) {
					return slog.String(a.Key, tokenRegex.ReplaceAllString(val, "<REDACTED>"))
				}
			}
			return a
		},
	}

	jsonHandler := slog.NewJSONHandler(bufferedWriter, opts)
	handler := &otelHandler{jsonHandler}
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

// otelHandler wraps a handler to inject trace information from context
type otelHandler struct {
	slog.Handler
}

func (h *otelHandler) Handle(ctx context.Context, r slog.Record) error {
	if ctx == nil {
		return h.Handler.Handle(ctx, r)
	}

	spanContext := trace.SpanContextFromContext(ctx)
	if spanContext.HasTraceID() {
		r.AddAttrs(
			slog.String("trace_id", spanContext.TraceID().String()),
			slog.String("span_id", spanContext.SpanID().String()),
		)
	}

	return h.Handler.Handle(ctx, r)
}
