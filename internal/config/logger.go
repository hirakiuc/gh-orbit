package config

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"

	"go.opentelemetry.io/otel/trace"
)

// greedy token regex to match prefix + alphanumeric + underscore body
var tokenRegex = regexp.MustCompile(`(ghp_|github_pat_|gho_|ghs_|ghr_)[a-zA-Z0-9_]+`)

// RedactSecrets masks known GitHub tokens in the given string.
func RedactSecrets(s string) string {
	return tokenRegex.ReplaceAllString(s, "<REDACTED>")
}

// SetupLogger initializes a structured slog logger.
// If sink is nil, it defaults to orbit.log in the state directory.
func SetupLogger(level *slog.LevelVar, sink io.Writer) (*slog.Logger, func() error, error) {
	var writer io.Writer
	var closer func() error

	if sink != nil {
		writer = sink
		closer = func() error { return nil }
	} else {
		stateDir, err := ResolveStateDir()
		if err != nil {
			return nil, nil, err
		}
		path := filepath.Join(stateDir, "orbit.log")

		// 1. Log Rotation Guard (10MB)
		flags := os.O_CREATE | os.O_APPEND | os.O_WRONLY
		if info, err := os.Stat(path); err == nil {
			if info.Size() > 10*1024*1024 {
				flags |= os.O_TRUNC
			}
		}

		if err := EnsurePrivateDir(filepath.Dir(path)); err != nil {
			return nil, nil, fmt.Errorf("failed to secure log directory: %w", err)
		}

		file, err := os.OpenFile(path, flags, 0o600) // #nosec G304: Internal XDG path
		if err != nil {
			return nil, nil, fmt.Errorf("failed to open log file: %w", err)
		}

		bufferedWriter := bufio.NewWriter(file)
		writer = bufferedWriter
		closer = func() error {
			if err := bufferedWriter.Flush(); err != nil {
				_ = file.Close()
				return err
			}
			return file.Close()
		}
	}

	opts := &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Value.Kind() == slog.KindString {
				val := a.Value.String()
				if tokenRegex.MatchString(val) {
					return slog.String(a.Key, RedactSecrets(val))
				}
			}
			return a
		},
	}

	jsonHandler := slog.NewJSONHandler(writer, opts)
	handler := &otelHandler{jsonHandler}
	logger := slog.New(handler)

	return logger, closer, nil
}

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
