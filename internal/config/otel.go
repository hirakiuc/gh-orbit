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
	"go.opentelemetry.io/otel/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// SetupOTel bootstraps the OpenTelemetry infrastructure with a local JSON file exporter.
func SetupOTel(ctx context.Context, version string) (*sdktrace.TracerProvider, func(), error) {
	path, err := resolveTracePath()
	if err != nil {
		return nil, nil, err
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, fmt.Errorf("failed to create trace directory: %w", err)
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) // #nosec G304: Path is internally resolved following XDG specs
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open trace file: %w", err)
	}

	// 1. Create the JSON exporter
	exporter, err := stdouttrace.New(
		stdouttrace.WithWriter(file),
		stdouttrace.WithPrettyPrint(),
	)
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}

	// 2. Define Resource (Service Metadata)
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName("gh-orbit"),
			semconv.ServiceVersion(version),
		),
	)
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}

	// 3. Create Tracer Provider with SimpleSpanProcessor for immediate flushing (CLI standard)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)),
		sdktrace.WithResource(res),
	)

	// Register global provider
	otel.SetTracerProvider(tp)

	cleanup := func() {
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

func resolveTracePath() (string, error) {
	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		stateHome = filepath.Join(home, ".local", "state")
	}

	return filepath.Clean(filepath.Join(stateHome, "gh-orbit", "orbit.traces.json")), nil
}

// GetTracer returns a tracer instance for the package.
func GetTracer() trace.Tracer {
	return otel.Tracer("github.com/hirakiuc/gh-orbit")
}
