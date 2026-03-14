package api

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// AppLifecycle manages the global application context and signal handling.
type AppLifecycle struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// NewAppLifecycle creates a new lifecycle manager linked to system signals.
func NewAppLifecycle(parent context.Context) *AppLifecycle {
	ctx, cancel := context.WithCancel(parent)

	l := &AppLifecycle{
		ctx:    ctx,
		cancel: cancel,
	}

	// Handle termination signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		select {
		case <-sigChan:
			cancel()
		case <-ctx.Done():
			// Already canceled
		}
	}()

	return l
}

// Context returns the supervisor context for background workers.
func (l *AppLifecycle) Context() context.Context {
	return l.ctx
}

// Shutdown manually triggers the lifecycle cancellation.
func (l *AppLifecycle) Shutdown() {
	l.cancel()
}
