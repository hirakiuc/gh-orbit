package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// SetupOTel bootstraps the OpenTelemetry infrastructure with a local JSON file exporter.
func SetupOTel(ctx context.Context, version string) (*sdktrace.TracerProvider, func(), error) {
	stateDir, err := ResolveStateDir()
	if err != nil {
		return nil, nil, err
	}
	path := filepath.Join(stateDir, "orbit.traces.json")

	// 1. Trace Rotation Guard (10MB)
	flags := os.O_CREATE | os.O_APPEND | os.O_WRONLY
	if info, err := os.Stat(path); err == nil {
		if info.Size() > 10*1024*1024 {
			// Atomically truncate if too large to prevent unbounded growth
			flags |= os.O_TRUNC
		}
	}

	// Create parent directory with strict permissions
	if err := EnsurePrivateDir(filepath.Dir(path)); err != nil {
		return nil, nil, fmt.Errorf("failed to secure trace directory: %w", err)
	}

	file, err := os.OpenFile(path, flags, 0o600) // #nosec G304: Internal XDG path
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open trace file: %w", err)
	}

	// 2. Create the JSON exporter
	exporter, err := stdouttrace.New(
		stdouttrace.WithWriter(file),
	)
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}

	// 3. Define Resource (Service Metadata)
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			"", // Omit schema URL to avoid conflicts with Default resource
			semconv.ServiceName("gh-orbit"),
			semconv.ServiceVersion(version),
		),
	)
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}

	// 4. Create Tracer Provider with Batcher for high-performance async I/O
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	// Register global provider
	otel.SetTracerProvider(tp)

	cleanup := func() {
		// Final flush ensured by Two-Phase Shutdown in main.go
		shutdownCtx := context.Background()
		if err := tp.ForceFlush(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "Error flushing tracer provider: %v\n", err)
		}
		if err := tp.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "Error shutting down tracer provider: %v\n", err)
		}
		_ = file.Close()
	}

	return tp, cleanup, nil
}

// GetTracer returns a tracer instance for the package.
func GetTracer() trace.Tracer {
	return otel.Tracer("github.com/hirakiuc/gh-orbit")
}
