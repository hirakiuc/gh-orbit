package api

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestAppLifecycle_ContextInheritance(t *testing.T) {
	type key string
	k := key("test-key")
	v := "test-value"

	parent := context.WithValue(context.Background(), k, v)
	l := NewAppLifecycle(parent)
	defer l.Shutdown()

	ctx := l.Context()

	// Verify value inheritance
	assert.Equal(t, v, ctx.Value(k))

	// Verify cancellation propagation
	parentCtx, cancel := context.WithCancel(context.Background())
	l2 := NewAppLifecycle(parentCtx)

	cancel()

	select {
	case <-l2.Context().Done():
		// Success
	case <-time.After(100 * time.Millisecond):
		t.Fatal("lifecycle context was not canceled when parent was canceled")
	}
}

func TestAppLifecycle_Shutdown(t *testing.T) {
	l := NewAppLifecycle(context.Background())
	l.Shutdown()

	select {
	case <-l.Context().Done():
		// Success
	case <-time.After(100 * time.Millisecond):
		t.Fatal("lifecycle context was not canceled after Shutdown()")
	}
}
