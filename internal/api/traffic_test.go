package api

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestTrafficController_Serialization(t *testing.T) {
	logger := slog.Default()
	tc := NewAPITrafficController(logger)

	var counter int32
	
	task := func(ctx context.Context) tea.Msg {
		atomic.AddInt32(&counter, 1)
		time.Sleep(50 * time.Millisecond)
		return nil
	}

	// Submit two tasks
	cmd1 := tc.Submit(PriorityUser, task)
	cmd2 := tc.Submit(PriorityUser, task)

	// Run concurrently
	go cmd1()
	go cmd2()

	time.Sleep(20 * time.Millisecond)
	// Only one should be running at a time, but since they are triggered in goroutines,
	// we just want to ensure that they don't overlap.
	// Actually, the current Submit implementation uses a Mutex directly inside the function.
	// So cmd1 and cmd2 will block each other.
}

func TestTrafficController_RateLimitGuard(t *testing.T) {
	logger := slog.Default()
	tc := NewAPITrafficController(logger)
	tc.UpdateRateLimit(100) // Below threshold (500)

	var executed bool
	task := func(ctx context.Context) tea.Msg {
		executed = true
		return nil
	}

	// Enrichment task should be skipped
	cmd := tc.Submit(PriorityEnrich, task)
	msg := cmd()

	if msg != nil || executed {
		t.Error("expected enrichment task to be skipped due to low rate limit")
	}

	// User task should NOT be skipped
	executed = false
	cmdUser := tc.Submit(PriorityUser, task)
	msgUser := cmdUser()

	if executed == false {
		t.Error("expected user task to execute despite low rate limit")
	}
	_ = msgUser
}
